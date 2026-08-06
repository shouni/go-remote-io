package gcs

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/storage"

	"github.com/shouni/go-remote-io/remoteio"
)

// urlSigner は GCS の署名付き URL を生成する remoteio.URLSigner です。
type urlSigner struct {
	client *storage.Client
}

var _ remoteio.URLSigner = (*urlSigner)(nil)

// NewURLSigner は GCS 用の署名付き URL 生成器を初期化します。
func NewURLSigner(client *storage.Client) remoteio.URLSigner {
	return &urlSigner{client: client}
}

// GenerateSignedURL は GCS URI に対応する署名付きURLを生成します。
// スキームは厳格で、gs:// 以外は受け付けません。
func (s *urlSigner) GenerateSignedURL(_ context.Context, path string, method string, expires time.Duration) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("GCSクライアントが初期化されていないため、署名付きURLを生成できません")
	}

	if !remoteio.IsGCSURI(path) {
		return "", fmt.Errorf("署名付きURLはGCS URI (gs://...) のみサポートされます: %s", path)
	}

	bucketName, objectPath, err := remoteio.ParseRemoteURI(path)
	if err != nil {
		return "", fmt.Errorf("GCS URIの解析に失敗: %w", err)
	}

	opts := &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  method,
		Expires: time.Now().Add(expires),
	}

	return s.client.Bucket(bucketName).SignedURL(objectPath, opts)
}
