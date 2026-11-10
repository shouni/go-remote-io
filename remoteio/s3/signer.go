package s3

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/shouni/go-remote-io/remoteio"
)

// SignURL generates a signed URL for an S3 URI.
//
// It implements remoteio.Signer. The scheme is strict, s3:// only, and only the
// GET / PUT methods are supported.
func (h *Handler) SignURL(ctx context.Context, uri, method string, expires time.Duration) (string, error) {
	bucket, key, err := h.parseObjectURI(uri)
	if err != nil {
		return "", fmt.Errorf("generate signed URL: %w", err)
	}

	presign := awss3.NewPresignClient(h.client)
	switch strings.ToUpper(method) {
	case "GET":
		request, err := presign.PresignGetObject(ctx, &awss3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		}, awss3.WithPresignExpires(expires))
		if err != nil {
			return "", fmt.Errorf("generate S3 GET signed URL (URI: %s): %w", uri, err)
		}
		return request.URL, nil
	case "PUT":
		request, err := presign.PresignPutObject(ctx, &awss3.PutObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		}, awss3.WithPresignExpires(expires))
		if err != nil {
			return "", fmt.Errorf("generate S3 PUT signed URL (URI: %s): %w", uri, err)
		}
		return request.URL, nil
	default:
		return "", fmt.Errorf("unsupported HTTP method: %s (only GET or PUT are supported): %w", method, remoteio.ErrNotSupported)
	}
}
