package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"google.golang.org/api/iterator"
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

// =================================================================
// コアロジック (実装)
// =================================================================

// Open は指定されたファイルパスに対応するリーダーを返します。
func (r *UniversalInputReader) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	if IsGCSURI(path) {
		return r.openGCSObject(ctx, path)
	}
	if IsS3URI(path) {
		return r.openS3Object(ctx, path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ローカルファイルのオープンに失敗しました: %w", err)
	}
	return file, nil
}

// List は指定されたパス内のファイルを再帰的にリストし、コールバック関数に渡します。
func (r *UniversalInputReader) List(ctx context.Context, path string, callback func(string) error) error {
	if IsGCSURI(path) {
		return r.listGCSObjects(ctx, path, callback)
	}
	if IsS3URI(path) {
		return r.listS3Objects(ctx, path, callback)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("ローカルディレクトリの読み込みに失敗しました (path: %s): %w", path, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			fullPath := filepath.Join(path, entry.Name())
			if err := callback(fullPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// =================================================================
// GCS / S3 内部実装
// =================================================================

// openGCSObject は指定された GCS URI に対応するリーダーを返します。
func (r *UniversalInputReader) openGCSObject(ctx context.Context, gcsURI string) (io.ReadCloser, error) {
	if r.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントが未初期化です (URI: %s)", gcsURI)
	}
	bucketName, objectName, err := ParseRemoteURI(gcsURI)
	if err != nil {
		return nil, err
	}
	if objectName == "" {
		return nil, fmt.Errorf("オブジェクト名が空です: %s", gcsURI)
	}

	rc, err := r.gcsClient.Bucket(bucketName).Object(objectName).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, fmt.Errorf("GCSオブジェクトが見つかりません (URI: %s): %w", gcsURI, os.ErrNotExist)
		}
		return nil, fmt.Errorf("GCS読み込み失敗 (URI: %s): %w", gcsURI, err)
	}
	return rc, nil
}

// listGCSObjects は指定された GCS URI に対応するオブジェクトを再帰的にリストし、コールバック関数に渡します。
func (r *UniversalInputReader) listGCSObjects(ctx context.Context, gcsURI string, callback func(string) error) error {
	if r.gcsClient == nil {
		return fmt.Errorf("GCSクライアントが未初期化です (URI: %s)", gcsURI)
	}
	bucketName, prefix, err := ParseRemoteURI(gcsURI)
	if err != nil {
		return err
	}

	// プレフィックスをそのまま使用することで、前方一致による柔軟な検索を可能にします。
	it := r.gcsClient.Bucket(bucketName).Objects(ctx, &storage.Query{Prefix: prefix})
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("GCSリスト取得失敗 (イテレーション中, URI: %s): %w", gcsURI, err)
		}
		// プレフィックス自身がディレクトリを示す0バイトオブジェクトとして返される場合があるため、
		// リスト結果からは除外します。
		if attrs.Name == prefix {
			continue
		}
		fullPath := BuildGCSURI(bucketName, attrs.Name)
		if err := callback(fullPath); err != nil {
			return err
		}
	}
	return nil
}

// openS3Object は指定された S3 URI に対応するリーダーを返します。
func (r *UniversalInputReader) openS3Object(ctx context.Context, s3URI string) (io.ReadCloser, error) {
	if r.s3Client == nil {
		return nil, fmt.Errorf("S3クライアントが未初期化です (URI: %s)", s3URI)
	}
	bucketName, objectPath, err := ParseRemoteURI(s3URI)
	if err != nil {
		return nil, err
	}
	if objectPath == "" {
		return nil, fmt.Errorf("オブジェクト名が空です: %s", s3URI)
	}

	result, err := r.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectPath),
	})
	if err != nil {
		if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
			return nil, fmt.Errorf("S3オブジェクトが見つかりません (URI: %s): %w", s3URI, os.ErrNotExist)
		}
		return nil, fmt.Errorf("S3読み込み失敗 (URI: %s): %w", s3URI, err)
	}
	return result.Body, nil
}

// listS3Objects は指定された S3 URI に対応するオブジェクトを再帰的にリストし、コールバック関数に渡します。
func (r *UniversalInputReader) listS3Objects(ctx context.Context, s3URI string, callback func(string) error) error {
	if r.s3Client == nil {
		return fmt.Errorf("S3クライアントが未初期化です (URI: %s)", s3URI)
	}
	bucketName, prefix, err := ParseRemoteURI(s3URI)
	if err != nil {
		return err
	}

	// プレフィックスをそのまま使用することで、前方一致による柔軟な検索を可能にします。
	paginator := s3.NewListObjectsV2Paginator(r.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("S3リスト取得失敗 (ページネーション中, URI: %s): %w", s3URI, err)
		}
		for _, obj := range output.Contents {
			// プレフィックス自身がディレクトリを示す0バイトオブジェクトとして返される場合があるため、
			// リスト結果からは除外します。
			if *obj.Key == prefix {
				continue
			}
			fullPath := BuildS3URI(bucketName, *obj.Key)
			if err := callback(fullPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// Exists は指定されたパスにリソース（GCS、S3、またはローカルファイル）が存在するかを確認します。
func (r *UniversalInputReader) Exists(ctx context.Context, path string) (bool, error) {
	if IsGCSURI(path) {
		return r.existsGCS(ctx, path)
	}
	if IsS3URI(path) {
		return r.existsS3(ctx, path)
	}

	// ローカルファイルの存在確認
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("ローカルファイルのステータス取得に失敗しました: %w", err)
}

func (r *UniversalInputReader) existsGCS(ctx context.Context, gcsURI string) (bool, error) {
	if r.gcsClient == nil {
		return false, fmt.Errorf("GCSクライアントが未初期化です: %s", gcsURI)
	}
	bucketName, objectName, err := ParseRemoteURI(gcsURI)
	if err != nil {
		return false, err
	}

	_, err = r.gcsClient.Bucket(bucketName).Object(objectName).Attrs(ctx)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, storage.ErrObjectNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("GCS属性取得失敗: %w", err)
}

func (r *UniversalInputReader) existsS3(ctx context.Context, s3URI string) (bool, error) {
	if r.s3Client == nil {
		return false, fmt.Errorf("S3クライアントが未初期化です: %s", s3URI)
	}
	bucketName, objectPath, err := ParseRemoteURI(s3URI)
	if err != nil {
		return false, err
	}

	_, err = r.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectPath),
	})
	if err == nil {
		return true, nil
	}

	// S3のHeadObjectは、存在しない場合に404を返します
	var nf *types.NotFound
	if _, ok := errors.AsType[*types.NoSuchKey](err); ok || errors.As(err, &nf) {
		return false, nil
	}

	return false, fmt.Errorf("S3属性取得失敗: %w", err)
}
