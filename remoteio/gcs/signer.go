package gcs

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/storage"

	"github.com/shouni/go-remote-io/remoteio"
)

// zeroTime は、疑似ディレクトリのように更新時刻を持たない Entry のための値です。
var zeroTime time.Time

// SignURL は GCS URI に対応する署名付き URL を生成します。
//
// remoteio.Signer の実装です。v1 は URLSigner を独立したインターフェースにし、
// 専用の型 (urlSigner) とスキームごとの振り分け (signerRouter) を別に持っていました。
// ハンドラの任意機能にすることで、振り分けは Router の 1 箇所へ戻ります。
//
// スキームは厳格で、gs:// 以外は受け付けません。
func (h *Handler) SignURL(_ context.Context, uri, method string, expires time.Duration) (string, error) {
	bucket, object, err := h.parseObjectURI(uri)
	if err != nil {
		return "", fmt.Errorf("署名付きURLの生成に失敗しました: %w", err)
	}

	opts := &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  method,
		Expires: time.Now().Add(expires),
	}

	signed, err := h.client.Bucket(bucket).SignedURL(object, opts)
	if err != nil {
		return "", fmt.Errorf("GCS署名付きURLの生成に失敗しました (URI: %s): %w", uri, err)
	}
	return signed, nil
}

var _ remoteio.Signer = (*Handler)(nil)
