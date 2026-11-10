package gcs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"

	"github.com/shouni/go-remote-io/remoteio"
)

// Scheme is the scheme name this package is responsible for (without the separator).
const Scheme = remoteio.SchemeGCS

// Handler is the remoteio.Handler for Google Cloud Storage.
//
// It also implements the optional interfaces for server-side copy (remoteio.Copier)
// and signed URLs (remoteio.Signer).
type Handler struct {
	client *storage.Client
}

var (
	_ remoteio.Handler = (*Handler)(nil)
	_ remoteio.Copier  = (*Handler)(nil)
	_ remoteio.Signer  = (*Handler)(nil)
)

// NewHandler returns a handler wrapping a GCS client.
func NewHandler(client *storage.Client) *Handler {
	return &Handler{client: client}
}

// Scheme returns "gs".
func (h *Handler) Scheme() string { return Scheme }

// Open returns a read stream for a GCS object.
// When the object does not exist, the error contains remoteio.ErrNotExist.
func (h *Handler) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	bucket, object, err := h.parseObjectURI(uri)
	if err != nil {
		return nil, err
	}

	rc, err := h.client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, notExist(uri)
		}
		return nil, fmt.Errorf("GCS read failed (URI: %s): %w", uri, err)
	}
	return rc, nil
}

// Stat returns the metadata of a GCS object.
// When it is not found, the error contains remoteio.ErrNotExist, as with Open.
func (h *Handler) Stat(ctx context.Context, uri string) (remoteio.ObjectInfo, error) {
	bucket, object, err := h.parseObjectURI(uri)
	if err != nil {
		return remoteio.ObjectInfo{}, err
	}

	attrs, err := h.client.Bucket(bucket).Object(object).Attrs(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return remoteio.ObjectInfo{}, notExist(uri)
		}
		return remoteio.ObjectInfo{}, fmt.Errorf("GCS attrs failed (URI: %s): %w", uri, err)
	}

	return remoteio.ObjectInfo{
		URI:         uri,
		Size:        attrs.Size,
		ModTime:     attrs.Updated,
		ContentType: attrs.ContentType,
		Metadata:    attrs.Metadata,
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
		query := &storage.Query{Prefix: prefix, Delimiter: opts.Delimiter}
		it := h.client.Bucket(bucket).Objects(ctx, query)
		for {
			attrs, err := it.Next()
			if errors.Is(err, iterator.Done) {
				return
			}
			if err != nil {
				yield(remoteio.Entry{}, fmt.Errorf("GCS list failed (during iteration, URI: %s): %w", uri, err))
				return
			}

			// With a delimiter, a pseudo-directory has an empty Name and is in Prefix.
			entry := remoteio.Entry{
				Size:    attrs.Size,
				ModTime: attrs.Updated,
			}
			key := attrs.Name
			if key == "" {
				key = attrs.Prefix
				entry.IsPrefix = true
				entry.Size, entry.ModTime = 0, zeroTime
			}
			if key == "" || key == prefix {
				continue
			}

			entry.URI = remoteio.BuildURI(Scheme, bucket, key)
			entry.Name = strings.TrimPrefix(key, prefix)
			if !yield(entry, nil) {
				return
			}
		}
	}
}

// Write writes to a GCS object.
//
// storage.Writer.Close() must not be called as is on the failure path. It is the API
// that "completes" the upload, so calling it leaves the failed write behind as a
// partial object. The abort procedure is in the comment on the io.Copy failure path.
// As long as it aborts correctly, no truncated object is left behind.
func (h *Handler) Write(ctx context.Context, uri string, src io.Reader, opts remoteio.WriteOptions) error {
	bucket, object, err := h.parseObjectURI(uri)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "GCS write started",
		slog.String("uri", uri),
		slog.String("content_type", opts.ContentType),
		slog.String("disposition", opts.ContentDisposition),
		slog.String("cache_control", opts.CacheControl),
	)

	// A ctx for aborting the upload on the failure path. cancel running after a
	// successful Close has no effect.
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()

	handle := h.client.Bucket(bucket).Object(object)
	if opts.IfNotExists {
		// Leave the decision to the storage-side precondition, so no other process can
		// slip in between the existence check and the write.
		handle = handle.If(storage.Conditions{DoesNotExist: true})
	}

	wc := handle.NewWriter(wctx)
	wc.ContentType = opts.ContentType
	if opts.ContentDisposition != "" {
		wc.ContentDisposition = opts.ContentDisposition
	}
	if opts.CacheControl != "" {
		wc.CacheControl = opts.CacheControl
	}
	if len(opts.Metadata) > 0 {
		wc.Metadata = opts.Metadata
	}

	// n tells whether the Writer was opened. When io.Copy cannot read a single byte it
	// never calls Write, and then the SDK has started neither its internal goroutine nor a request.
	n, copyErr := io.Copy(wc, src)
	if copyErr != nil {
		cancel()

		if n > 0 {
			// Abort on the pipe side. storage.Writer uploads with an internal goroutine
			// reading the body from an io.Pipe, so setting an error on the pipe makes the
			// request certain to fail, and no object is created.
			// Relying on ctx cancellation leaves it to the transport, and in fact the GCS
			// fake completes uploads even with a cancelled ctx.
			_ = wc.CloseWithError(copyErr) //nolint:staticcheck // the proper way to abort; see the comment below
			// CloseWithError does not wait for the goroutine to finish, so Close follows.
			// The pipe already carries an error, so the upload never completes, and the
			// <-donec inside Close waits until cleanup is done. Without it, Write would
			// return leaving the goroutine running.
			_ = wc.Close()
		}
		// When n == 0 the Writer has not been opened. Calling Close here would run
		// openWriter and create a zero-byte object, so it is left alone.

		return fmt.Errorf("write content to GCS (URI: %s): %w", uri, copyErr)
	}

	if err := wc.Close(); err != nil {
		if opts.IfNotExists && isPreconditionFailed(err) {
			return fmt.Errorf("GCS object already exists (URI: %s): %w", uri, remoteio.ErrExist)
		}
		return fmt.Errorf("close GCS writer (URI: %s, error during upload): %w", uri, err)
	}

	slog.DebugContext(ctx, "GCS write finished", slog.String("uri", uri))
	return nil
}

// Delete removes a GCS object. Absence is not an error.
func (h *Handler) Delete(ctx context.Context, uri string) error {
	bucket, object, err := h.parseObjectURI(uri)
	if err != nil {
		return err
	}
	err = h.client.Bucket(bucket).Object(object).Delete(ctx)
	if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return fmt.Errorf("delete GCS object (URI: %s): %w", uri, err)
	}
	return nil
}

// CopyTo duplicates an object with a GCS server-side copy.
//
// It implements remoteio.Copier. The contents never travel through the client, so
// neither transfer volume nor time is spent, whether across buckets or within one.
func (h *Handler) CopyTo(ctx context.Context, src, dst string) error {
	srcBucket, srcObject, err := h.parseObjectURI(src)
	if err != nil {
		return err
	}
	dstBucket, dstObject, err := h.parseObjectURI(dst)
	if err != nil {
		return err
	}

	srcHandle := h.client.Bucket(srcBucket).Object(srcObject)
	if _, err := h.client.Bucket(dstBucket).Object(dstObject).CopierFrom(srcHandle).Run(ctx); err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return notExist(src)
		}
		return fmt.Errorf("GCS server-side copy (%s -> %s): %w", src, dst, err)
	}
	return nil
}

// parseBucketURI validates the URI and splits it into a bucket name and a prefix
// (the object name may be empty). Used for operations where an empty prefix is meaningful, such as listings.
func (h *Handler) parseBucketURI(uri string) (bucket, prefix string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("GCS client is not initialized (URI: %s): %w", uri, remoteio.ErrClosed)
	}
	return remoteio.ParseBucketURI(Scheme, uri)
}

// parseObjectURI validates the URI and splits it into a bucket name and an object name.
// See remoteio.ParseObjectURI for why URIs with an empty object name (gs://bucket) are rejected.
func (h *Handler) parseObjectURI(uri string) (bucket, object string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("GCS client is not initialized (URI: %s): %w", uri, remoteio.ErrClosed)
	}
	return remoteio.ParseObjectURI(Scheme, uri)
}

// notExist returns "not found" in a form that contains remoteio.ErrNotExist.
// That callers can detect it with errors.Is whatever the scheme is the premise of this abstraction.
func notExist(uri string) error {
	return fmt.Errorf("GCS object not found (URI: %s): %w", uri, remoteio.ErrNotExist)
}

// isPreconditionFailed reports whether the response is a failed DoesNotExist precondition.
func isPreconditionFailed(err error) bool {
	if apiErr, ok := errors.AsType[*googleapi.Error](err); ok {
		return apiErr.Code == 412
	}
	return false
}

// errSeq returns an iterator that reports, once, a failure before iteration started.
func errSeq(err error) iter.Seq2[remoteio.Entry, error] {
	return func(yield func(remoteio.Entry, error) bool) { yield(remoteio.Entry{}, err) }
}
