package s3_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio/s3"
)

// 署名付き URL のスキームは厳格であること。
func TestURLSignerRejectsForeignScheme(t *testing.T) {
	t.Parallel()

	signer := s3.NewURLSigner(nil)

	for _, path := range []string{"gs://bucket/obj", "/local/path", "https://example.com/x"} {
		_, err := signer.GenerateSignedURL(context.Background(), path, "GET", time.Minute)
		require.Error(t, err, path)
	}
}

// nil クライアントで NewPresignClient を呼ぶと AWS SDK がパニックするため、
// 構築時にガードして「初期化されていない」エラーで返すこと。
func TestURLSignerWithoutClient(t *testing.T) {
	t.Parallel()

	signer := s3.NewURLSigner(nil)

	_, err := signer.GenerateSignedURL(context.Background(), "s3://bucket/obj", "GET", time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "S3クライアントが初期化されていない")
}

// GET / PUT 以外は明確に拒否すること（対応していない旨が呼び出し側に伝わる）。
func TestURLSignerRejectsUnsupportedMethod(t *testing.T) {
	t.Parallel()

	signer := s3.NewURLSigner(nil)

	_, err := signer.GenerateSignedURL(context.Background(), "s3://bucket/obj", "DELETE", time.Minute)
	require.Error(t, err)
}
