package gcs

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/storage"

	"github.com/shouni/go-remote-io/remoteio"
)

// zeroTime is the value for Entry values without a modification time, such as pseudo-directories.
var zeroTime time.Time

// SignURL generates a signed URL for a GCS URI.
//
// It implements remoteio.Signer. The scheme is strict: only gs:// is accepted.
func (h *Handler) SignURL(_ context.Context, uri, method string, expires time.Duration) (string, error) {
	bucket, object, err := h.parseObjectURI(uri)
	if err != nil {
		return "", fmt.Errorf("generate signed URL: %w", err)
	}

	opts := &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  method,
		Expires: time.Now().Add(expires),
	}

	signed, err := h.client.Bucket(bucket).SignedURL(object, opts)
	if err != nil {
		return "", fmt.Errorf("generate GCS signed URL (URI: %s): %w", uri, err)
	}
	return signed, nil
}

var _ remoteio.Signer = (*Handler)(nil)
