package gcs_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio/gcs"
)

// 署名付き URL のスキームは厳格であること。
// s3:// を渡して GCS の署名を返してしまうと、呼び出し側は壊れた URL を
// 正常系として配ることになります。
func TestURLSignerRejectsForeignScheme(t *testing.T) {
	t.Parallel()

	signer := gcs.NewURLSigner(nil)

	for _, path := range []string{"s3://bucket/obj", "/local/path", "https://example.com/x"} {
		_, err := signer.GenerateSignedURL(context.Background(), path, "GET", time.Minute)
		require.Error(t, err, path)
	}
}

// クライアント未初期化のまま署名を求められたら、その旨を返すこと。
func TestURLSignerWithoutClient(t *testing.T) {
	t.Parallel()

	signer := gcs.NewURLSigner(nil)

	_, err := signer.GenerateSignedURL(context.Background(), "gs://bucket/obj", "GET", time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GCSクライアントが初期化されていない")
}
