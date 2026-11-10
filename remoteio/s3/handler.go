package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/shouni/go-remote-io/remoteio"
)

// Scheme is the scheme name this package is responsible for (without the separator).
const Scheme = remoteio.SchemeS3

// Handler is the remoteio.Handler for Amazon S3.
//
// It also implements the optional interfaces for server-side copy (remoteio.Copier)
// and signed URLs (remoteio.Signer).
type Handler struct {
	client *awss3.Client
	// transfer handles non-Seeker streams and multipart uploads.
	//
	// The Uploader / Downloader in the old feature/s3/manager are deprecated, and
	// feature/s3/transfermanager is the successor (aws-sdk-go-v2 discussion #3306).
	// The successor is still v0.x and its API may move, but only one place in this
	// ecosystem uses s3://, so not carrying a deprecated API is preferred over the
	// cost of keeping up.
	transfer *transfermanager.Client
}

var (
	_ remoteio.Handler = (*Handler)(nil)
	_ remoteio.Copier  = (*Handler)(nil)
	_ remoteio.Signer  = (*Handler)(nil)
)

// NewHandler returns a handler wrapping an S3 client.
func NewHandler(client *awss3.Client) *Handler {
	h := &Handler{client: client}
	if client != nil {
		h.transfer = transfermanager.New(client)
	}
	return h
}

// Scheme returns "s3".
func (h *Handler) Scheme() string { return Scheme }

// Open returns a read stream for an S3 object.
// When the object does not exist, the error contains remoteio.ErrNotExist.
func (h *Handler) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	bucket, key, err := h.parseObjectURI(uri)
	if err != nil {
		return nil, err
	}

	result, err := h.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, notExist(uri)
		}
		return nil, fmt.Errorf("S3 read failed (URI: %s): %w", uri, err)
	}
	return result.Body, nil
}

// Stat returns the metadata of an S3 object.
// When it is not found, the error contains remoteio.ErrNotExist, as with Open.
func (h *Handler) Stat(ctx context.Context, uri string) (remoteio.ObjectInfo, error) {
	bucket, key, err := h.parseObjectURI(uri)
	if err != nil {
		return remoteio.ObjectInfo{}, err
	}

	output, err := h.client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return remoteio.ObjectInfo{}, notExist(uri)
		}
		return remoteio.ObjectInfo{}, fmt.Errorf("S3 attrs failed (URI: %s): %w", uri, err)
	}

	return remoteio.ObjectInfo{
		URI:         uri,
		Size:        aws.ToInt64(output.ContentLength),
		ModTime:     aws.ToTime(output.LastModified),
		ContentType: aws.ToString(output.ContentType),
		Metadata:    output.Metadata,
	}, nil
}

// List enumerates the objects under the prefix.
func (h *Handler) List(ctx context.Context, uri string, opts remoteio.ListOptions) iter.Seq2[remoteio.Entry, error] {
	// Router passes a normalized URI, but handlers are exported and can be called
	// directly. Normalization is idempotent, so it is applied here as well.
	uri = remoteio.ListPrefix(uri)

	bucket, prefix, err := h.parseBucketURI(uri)
	if err != nil {
		return errSeq(err)
	}

	return func(yield func(remoteio.Entry, error) bool) {
		input := &awss3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
			Prefix: aws.String(prefix),
		}
		if opts.Delimiter != "" {
			input.Delimiter = aws.String(opts.Delimiter)
		}

		paginator := awss3.NewListObjectsV2Paginator(h.client, input)
		for paginator.HasMorePages() {
			output, err := paginator.NextPage(ctx)
			if err != nil {
				yield(remoteio.Entry{}, fmt.Errorf("S3 list failed (during pagination, URI: %s): %w", uri, err))
				return
			}

			// With a delimiter, pseudo-directories are in CommonPrefixes rather than
			// Contents.
			for _, cp := range output.CommonPrefixes {
				key := aws.ToString(cp.Prefix)
				if key == "" || key == prefix {
					continue
				}
				entry := remoteio.Entry{
					URI:      remoteio.BuildURI(Scheme, bucket, key),
					Name:     strings.TrimPrefix(key, prefix),
					IsPrefix: true,
				}
				if !yield(entry, nil) {
					return
				}
			}

			for _, obj := range output.Contents {
				key := aws.ToString(obj.Key)
				if key == "" || key == prefix {
					continue
				}
				entry := remoteio.Entry{
					URI:     remoteio.BuildURI(Scheme, bucket, key),
					Name:    strings.TrimPrefix(key, prefix),
					Size:    aws.ToInt64(obj.Size),
					ModTime: aws.ToTime(obj.LastModified),
				}
				if !yield(entry, nil) {
					return
				}
			}
		}
	}
}

// Write writes to an S3 object.
//
// It goes through transfermanager rather than PutObject. PutObject failed to compute
// the pre-signing checksum unless the body was an io.Seeker, so against non-TLS
// endpoints (MinIO, R2, test fakes) nothing could be written at all.
// transfermanager handles non-Seeker streams, splits large objects into multipart
// uploads, and aborts the upload when it fails part-way, so it also meets the Handler
// contract that "the destination does not change unless the write succeeds".
func (h *Handler) Write(ctx context.Context, uri string, src io.Reader, opts remoteio.WriteOptions) error {
	bucket, key, err := h.parseObjectURI(uri)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "S3 write started",
		slog.String("uri", uri),
		slog.String("content_type", opts.ContentType),
		slog.String("disposition", opts.ContentDisposition),
		slog.String("cache_control", opts.CacheControl),
	)

	input := &transfermanager.UploadObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        src,
		ContentType: aws.String(opts.ContentType),
	}
	if opts.ContentDisposition != "" {
		input.ContentDisposition = aws.String(opts.ContentDisposition)
	}
	if opts.CacheControl != "" {
		input.CacheControl = aws.String(opts.CacheControl)
	}
	if len(opts.Metadata) > 0 {
		input.Metadata = opts.Metadata
	}
	if opts.IfNotExists {
		// Leave the decision to the storage-side conditional request, so no other process
		// can slip in between the existence check and the write.
		input.IfNoneMatch = aws.String("*")
	}

	if _, err := h.transfer.UploadObject(ctx, input); err != nil {
		if opts.IfNotExists && isPreconditionFailed(err) {
			return fmt.Errorf("S3 object already exists (URI: %s): %w", uri, remoteio.ErrExist)
		}
		return fmt.Errorf("write content to S3 (URI: %s): %w", uri, err)
	}

	slog.DebugContext(ctx, "S3 write finished", slog.String("uri", uri))
	return nil
}

// Delete removes an S3 object. Absence is not an error (S3's DeleteObject is itself idempotent).
func (h *Handler) Delete(ctx context.Context, uri string) error {
	bucket, key, err := h.parseObjectURI(uri)
	if err != nil {
		return err
	}
	if _, err := h.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("delete S3 object (URI: %s): %w", uri, err)
	}
	return nil
}

// CopyTo duplicates an object with an S3 server-side copy.
// It implements remoteio.Copier.
func (h *Handler) CopyTo(ctx context.Context, src, dst string) error {
	srcBucket, srcKey, err := h.parseObjectURI(src)
	if err != nil {
		return err
	}
	dstBucket, dstKey, err := h.parseObjectURI(dst)
	if err != nil {
		return err
	}

	if _, err := h.client.CopyObject(ctx, &awss3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(copySource(srcBucket, srcKey)),
	}); err != nil {
		if isNotFound(err) {
			return notExist(src)
		}
		return fmt.Errorf("S3 server-side copy (%s -> %s): %w", src, dst, err)
	}
	return nil
}

// copySource builds the "bucket/key" carried in the CopySource header.
//
// The key needs URL encoding, but the "/" separators must stay as they are.
// url.PathEscape turns "/" into %2F as well, so it is built as a URL Path and
// EscapedPath is taken.
func copySource(bucket, key string) string {
	u := url.URL{Path: "/" + bucket + "/" + key}
	return strings.TrimPrefix(u.EscapedPath(), "/")
}

// parseBucketURI validates the URI and splits it into a bucket name and a prefix
// (the object name may be empty). Used for operations where an empty prefix is meaningful, such as listings.
func (h *Handler) parseBucketURI(uri string) (bucket, prefix string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("S3 client is not initialized (URI: %s): %w", uri, remoteio.ErrClosed)
	}
	return remoteio.ParseBucketURI(Scheme, uri)
}

// parseObjectURI validates the URI and splits it into a bucket name and an object name.
func (h *Handler) parseObjectURI(uri string) (bucket, key string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("S3 client is not initialized (URI: %s): %w", uri, remoteio.ErrClosed)
	}
	return remoteio.ParseObjectURI(Scheme, uri)
}

// notExist returns "not found" in a form that contains remoteio.ErrNotExist.
func notExist(uri string) error {
	return fmt.Errorf("S3 object not found (URI: %s): %w", uri, remoteio.ErrNotExist)
}

// isNotFound reports whether the error means the object does not exist.
// HeadObject returns NotFound rather than NoSuchKey, so both are checked.
func isNotFound(err error) bool {
	if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
		return true
	}
	_, ok := errors.AsType[*types.NotFound](err)
	return ok
}

// isPreconditionFailed reports whether the response is a failed If-None-Match condition.
func isPreconditionFailed(err error) bool {
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		return apiErr.ErrorCode() == "PreconditionFailed"
	}
	return false
}

// errSeq returns an iterator that reports, once, a failure before iteration started.
func errSeq(err error) iter.Seq2[remoteio.Entry, error] {
	return func(yield func(remoteio.Entry, error) bool) { yield(remoteio.Entry{}, err) }
}
