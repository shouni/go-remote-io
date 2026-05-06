package remoteio

import (
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const DefaultContentType = "text/plain; charset=utf-8"

// UniversalIOWriter は GCSOutputWriter, S3OutputWriter, LocalOutputWriter のすべてを満たす具象型です。
type UniversalIOWriter struct {
	gcsClient *storage.Client
	s3Client  *s3.Client
}

// NewUniversalIOWriter は新しい UniversalIOWriter インスタンスを作成します。
// GCSクライアントとS3クライアントを注入します。
func NewUniversalIOWriter(gcsClient *storage.Client, s3Client *s3.Client) *UniversalIOWriter {
	return &UniversalIOWriter{gcsClient: gcsClient, s3Client: s3Client}
}

// Write は、パスに応じて適切なストレージへデータを書き込みます。
func (w *UniversalIOWriter) Write(ctx context.Context, path string, contentReader io.Reader, opts ...WriteOption) error {
	cfg := &writeConfig{
		contentType: DefaultContentType,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// 明示的に空文字が渡された場合のフォールバックを一元管理
	if cfg.contentType == "" {
		cfg.contentType = DefaultContentType
	}

	if IsGCSURI(path) {
		bucketName, objectPath, err := ParseRemoteURI(path)
		if err != nil {
			return fmt.Errorf("GCS URIのパースに失敗しました: %w", err)
		}
		return w.writeGCS(ctx, bucketName, objectPath, contentReader, cfg)
	}

	if IsS3URI(path) {
		bucketName, objectPath, err := ParseRemoteURI(path)
		if err != nil {
			return fmt.Errorf("S3 URIのパースに失敗しました: %w", err)
		}
		return w.writeS3(ctx, bucketName, objectPath, contentReader, cfg)
	}

	return w.writeLocal(ctx, path, contentReader, cfg)
}

// Delete はパスに応じて適切なストレージからリソースを削除します。
func (w *UniversalIOWriter) Delete(ctx context.Context, path string) error {
	if IsGCSURI(path) {
		bucketName, objectPath, err := ParseRemoteURI(path)
		if err != nil {
			return fmt.Errorf("GCS URIのパース失敗: %w", err)
		}
		return w.deleteGCS(ctx, bucketName, objectPath)
	}

	if IsS3URI(path) {
		bucketName, objectPath, err := ParseRemoteURI(path)
		if err != nil {
			return fmt.Errorf("S3 URIのパース失敗: %w", err)
		}
		return w.deleteS3(ctx, bucketName, objectPath)
	}

	return w.deleteLocal(path)
}
