package remoteio

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 1. GCS URLSigner のバリデーションテスト
func TestGCSURLSigner_Validation(t *testing.T) {
	ctx := context.Background()
	signer := NewGCSURLSigner(nil)

	t.Run("error on uninitialized client", func(t *testing.T) {
		// 実装の冒頭でクライアントのnilチェックを行うため、このエラーが最優先される
		url, err := signer.GenerateSignedURL(ctx, "gs://my-bucket/obj", "GET", 15*time.Minute)
		assert.Empty(t, url)
		assert.ErrorContains(t, err, "GCSクライアントが初期化されていない")
	})

	// URIバリデーションをテストしたい場合は、(本来は)モックのクライアントが必要ですが、
	// 現在の実装順序ではnilクライアントだとここには到達しません。
}

// 2. S3 URLSigner のバリデーションテスト
func TestS3URLSigner_Validation(t *testing.T) {
	ctx := context.Background()
	signer := NewS3URLSigner(nil)

	t.Run("error on uninitialized client", func(t *testing.T) {
		// S3も同様に、クライアントチェックが最初に行われる
		url, err := signer.GenerateSignedURL(ctx, "s3://my-bucket/obj", "GET", 15*time.Minute)
		assert.Empty(t, url)
		assert.ErrorContains(t, err, "S3クライアントが初期化されていない")
	})
}

// 3. インターフェース満足度のテスト
func TestURLSigner_InterfaceSatisfaction(t *testing.T) {
	var _ URLSigner = (*gcsURLSigner)(nil)
	var _ URLSigner = (*s3URLSigner)(nil)
}
