package remoteio

import (
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// DefaultContentType は、Content-Type が指定されなかった場合に使用されるデフォルト値です。
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

	return dispatchRemote(path,
		func(bucket, object string) error {
			return w.writeGCS(ctx, bucket, object, contentReader, cfg)
		},
		func(bucket, object string) error {
			return w.writeS3(ctx, bucket, object, contentReader, cfg)
		},
		func() error { return w.writeLocal(ctx, path, contentReader, cfg) },
	)
}

// dispatchRemote は URI スキームを見て GCS / S3 / ローカルのいずれかへ処理を振り分けます。
//
// Write と Delete がまったく同じ形の分岐を持っていたため、片方だけ直すと挙動がずれます。
// パースの失敗メッセージもここで一本化します。
func dispatchRemote(path string, gcs, s3 func(bucket, object string) error, local func() error) error {
	if IsGCSURI(path) || IsS3URI(path) {
		bucketName, objectPath, err := ParseRemoteURI(path)
		if err != nil {
			return fmt.Errorf("URIのパースに失敗しました (%s): %w", path, err)
		}
		if IsGCSURI(path) {
			return gcs(bucketName, objectPath)
		}
		return s3(bucketName, objectPath)
	}
	return local()
}

// Delete はパスに応じて適切なストレージからリソースを削除します。
func (w *UniversalIOWriter) Delete(ctx context.Context, path string) error {
	return dispatchRemote(path,
		func(bucket, object string) error { return w.deleteGCS(ctx, bucket, object) },
		func(bucket, object string) error { return w.deleteS3(ctx, bucket, object) },
		func() error { return w.deleteLocal(path) },
	)
}
