package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"google.golang.org/api/iterator"
)

// =================================================================
// 1. インターフェース定義
// =================================================================

// InputReader は、ローカルファイルパスまたはリモートURIから
// 読み取りストリームを開き、一覧を取得するためのインターフェースを定義します。
type InputReader interface {
	// Open は、指定されたパスから io.ReadCloser を返します。
	Open(ctx context.Context, filePath string) (io.ReadCloser, error)

	// List は、指定されたプレフィックス配下のファイル一覧をフルURIで返します。
	// ローカルパスの場合、指定されたディレクトリ直下のファイルのみを返し、再帰的な探索は行いません。
	// 【注意】大量のオブジェクトをリストするとメモリを著しく消費する可能性があるため、
	// 数千件を超えることが予想される場合は呼び出し方に注意してください。
	List(ctx context.Context, path string) ([]string, error)
}

// =================================================================
// 2. 具象構造体とコンストラクタ
// =================================================================

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
// 3. ヘルパー関数
// =================================================================

// ensureTrailingSlash は、プレフィックスが空でなく、かつ末尾にスラッシュがない場合にスラッシュを追加します。
func ensureTrailingSlash(prefix string) string {
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		return prefix + "/"
	}
	return prefix
}

// =================================================================
// 4. コアロジック (実装)
// =================================================================

// Open は、ファイルパスを検査し、ローカル、GCS、またはS3からストリームを開きます。
func (r *UniversalInputReader) Open(ctx context.Context, filePath string) (io.ReadCloser, error) {
	if IsGCSURI(filePath) {
		return r.openGCSObject(ctx, filePath)
	}
	if IsS3URI(filePath) {
		return r.openS3Object(ctx, filePath)
	}

	// ローカルファイルパスの処理
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("ローカルファイルのオープンに失敗しました: %w", err)
	}
	return file, nil
}

// List は、指定されたパスの種類に応じてファイル一覧を収集します。
func (r *UniversalInputReader) List(ctx context.Context, path string) ([]string, error) {
	if IsGCSURI(path) {
		return r.listGCSObjects(ctx, path)
	}
	if IsS3URI(path) {
		return r.listS3Objects(ctx, path)
	}

	// ローカルディレクトリのリスティング
	var files []string
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("ローカルディレクトリの読み込みに失敗しました: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, filepath.Join(path, entry.Name()))
		}
	}
	return files, nil
}

// =================================================================
// 5. GCS / S3 内部実装
// =================================================================

func (r *UniversalInputReader) openGCSObject(ctx context.Context, gcsURI string) (io.ReadCloser, error) {
	if r.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントが未初期化です (URI: %s)", gcsURI)
	}
	bucketName, objectName, err := ParseGCSURI(gcsURI)
	if err != nil {
		return nil, err
	}
	if objectName == "" {
		return nil, fmt.Errorf("オブジェクト名が空です: %s", gcsURI)
	}

	rc, err := r.gcsClient.Bucket(bucketName).Object(objectName).NewReader(ctx)
	if err != nil {
		// [Major] オブジェクト不在エラーを os.ErrNotExist でラップする
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, fmt.Errorf("GCSオブジェクトが見つかりません (URI: %s): %w", gcsURI, os.ErrNotExist)
		}
		return nil, fmt.Errorf("GCS読み込み失敗 (URI: %s): %w", gcsURI, err)
	}
	return rc, nil
}

func (r *UniversalInputReader) listGCSObjects(ctx context.Context, gcsURI string) ([]string, error) {
	if r.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントが未初期化です")
	}
	bucketName, prefix, err := ParseGCSURI(gcsURI)
	if err != nil {
		return nil, err
	}

	prefix = ensureTrailingSlash(prefix)

	var files []string
	it := r.gcsClient.Bucket(bucketName).Objects(ctx, &storage.Query{Prefix: prefix})
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("GCSリスト取得失敗: %w", err)
		}
		// [Minor] プレフィックス自体がオブジェクトとして存在する場合(0バイトオブジェクト等)を除外
		if attrs.Name == prefix {
			continue
		}
		files = append(files, fmt.Sprintf("gs://%s/%s", bucketName, attrs.Name))
	}
	return files, nil
}

func (r *UniversalInputReader) openS3Object(ctx context.Context, s3URI string) (io.ReadCloser, error) {
	if r.s3Client == nil {
		return nil, fmt.Errorf("S3クライアントが未初期化です (URI: %s)", s3URI)
	}
	bucketName, objectPath, err := ParseS3URI(s3URI)
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
		// [Major] NoSuchKeyエラーを os.ErrNotExist でラップする
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, fmt.Errorf("S3オブジェクトが見つかりません (URI: %s): %w", s3URI, os.ErrNotExist)
		}
		return nil, fmt.Errorf("S3読み込み失敗 (URI: %s): %w", s3URI, err)
	}
	return result.Body, nil
}

func (r *UniversalInputReader) listS3Objects(ctx context.Context, s3URI string) ([]string, error) {
	if r.s3Client == nil {
		return nil, fmt.Errorf("S3クライアントが未初期化です")
	}
	bucketName, prefix, err := ParseS3URI(s3URI)
	if err != nil {
		return nil, err
	}

	prefix = ensureTrailingSlash(prefix)

	paginator := s3.NewListObjectsV2Paginator(r.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(prefix),
	})

	var files []string
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("S3リスト取得失敗 (ページネーション中): %w", err)
		}
		for _, obj := range output.Contents {
			// [Minor] プレフィックス自体がオブジェクトとして存在する場合(0バイトオブジェクト等)を除外
			if *obj.Key == prefix {
				continue
			}
			files = append(files, fmt.Sprintf("s3://%s/%s", bucketName, *obj.Key))
		}
	}
	return files, nil
}
