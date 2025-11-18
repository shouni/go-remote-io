package remoteio

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/storage"
)

// URLSigner は、リモートストレージの署名付きURLを生成する機能を提供します。
type URLSigner interface {
	// GenerateSignedURL は、指定されたURIの署名付きURLを生成します。
	// ローカルパスなど、サポートされないURIの場合はエラーを返します。
	GenerateSignedURL(ctx context.Context, uri string, method string, expires time.Duration) (string, error)
}

// gcsURLSigner は GCS 用の URLSigner 実装です。
type gcsURLSigner struct {
	client *storage.Client
}

// NewGCSURLSigner は gcsURLSigner を初期化します。
func NewGCSURLSigner(client *storage.Client) URLSigner {
	return &gcsURLSigner{client: client}
}
func (s *gcsURLSigner) GenerateSignedURL(ctx context.Context, uri string, method string, expires time.Duration) (string, error) {
	// GCS URIでなければエラー
	if !IsGCSURI(uri) {
		return "", fmt.Errorf("署名付きURLはGCS URI (gs://...) のみサポートされます: %s", uri)
	}
	bucketName, objectPath, err := ParseGCSURI(uri)
	if err != nil {
		return "", fmt.Errorf("GCS URIの解析に失敗: %w", err)
	}
	opts := &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  method,
		Expires: time.Now().Add(expires),
	}
	// storage.Client を利用して署名
	return s.client.Bucket(bucketName).SignedURL(objectPath, opts)
}
