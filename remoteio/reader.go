package remoteio

import (
	"context"
	"io"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// UniversalInputReader は InputReader の具象実装であり、
// ローカルファイル、GCS オブジェクト、S3 オブジェクトを処理します。
type UniversalInputReader struct {
	gcsClient *storage.Client
	s3Client  *s3.Client
}

// NewUniversalInputReader は UniversalInputReader の新しいインスタンスを作成します。
func NewUniversalInputReader(gcsClient *storage.Client, s3Client *s3.Client) *UniversalInputReader {
	return &UniversalInputReader{
		gcsClient: gcsClient,
		s3Client:  s3Client,
	}
}

// Open は指定されたファイルパスに対応するリーダーを返します。
func (r *UniversalInputReader) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	if IsGCSURI(path) {
		return r.openGCS(ctx, path)
	}
	if IsS3URI(path) {
		return r.openS3(ctx, path)
	}

	return r.openLocal(path)
}

// List は指定された path に応じたリソース一覧を取得し、コールバック関数に渡します。
func (r *UniversalInputReader) List(ctx context.Context, path string, callback func(string) error) error {
	if IsGCSURI(path) {
		return r.listGCS(ctx, path, callback)
	}
	if IsS3URI(path) {
		return r.listS3(ctx, path, callback)
	}

	return r.listLocal(path, callback)
}

// Exists は指定されたパスにリソース（GCS、S3、またはローカルファイル）が存在するかを確認します。
func (r *UniversalInputReader) Exists(ctx context.Context, path string) (bool, error) {
	if IsGCSURI(path) {
		return r.existsGCS(ctx, path)
	}
	if IsS3URI(path) {
		return r.existsS3(ctx, path)
	}

	return r.existsLocal(path)
}
