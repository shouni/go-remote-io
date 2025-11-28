package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// =================================================================
// 1. インターフェース定義
// =================================================================

// InputReader は、ローカルファイルパスまたはリモートURIから
// 読み取りストリームを開くためのインターフェースを定義します。
type InputReader interface {
	// Open は、指定されたパスから io.ReadCloser を返します。
	Open(ctx context.Context, filePath string) (io.ReadCloser, error)
}

// =================================================================
// 2. 具象構造体とコンストラクタ
// =================================================================

// UniversalInputReader は InputReader の具象実装であり、
// ローカルファイル、GCS オブジェクト、S3 オブジェクトの読み込みを処理します。
type UniversalInputReader struct {
	gcsClient *storage.Client
	s3Client  *s3.Client
}

// NewUniversalInputReader は UniversalInputReader の新しいインスタンスを作成します。
// 依存関係として GCS クライアントと S3 クライアントを注入します。
func NewUniversalInputReader(gcsClient *storage.Client, s3Client *s3.Client) *UniversalInputReader {
	return &UniversalInputReader{
		gcsClient: gcsClient,
		s3Client:  s3Client,
	}
}

// =================================================================
// 3. コアロジック (実装)
// =================================================================

// Open は、ファイルパスを検査し、ローカルファイル、GCS、またはS3からストリームを開きます。
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

// openGCSObject は、GCS URI からオブジェクトを読み込み、io.ReadCloser を返します。
func (r *UniversalInputReader) openGCSObject(ctx context.Context, gcsURI string) (io.ReadCloser, error) {
	if r.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントが初期化されていないため、GCSオブジェクトを読み込めません (URI: %s)", gcsURI)
	}

	// URIのパースロジック (util.go の ParseGCSURI を使用)
	bucketName, objectName, err := ParseGCSURI(gcsURI)
	if err != nil {
		return nil, err
	}

	// util.goのParseGCSURIは gs://bucket を許容するため、InputReaderとして単一オブジェクトの
	// 読み込みに特化させるため、オブジェクト名が空でないことを再度チェックします。
	if objectName == "" {
		return nil, fmt.Errorf("無効なGCS URI形式: %s (オブジェクト名が空です。必須形式: gs://bucket-name/object-name)", gcsURI)
	}

	// GCS オブジェクトリーダーを作成
	rc, err := r.gcsClient.Bucket(bucketName).Object(objectName).NewReader(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return nil, fmt.Errorf("GCSオブジェクトが見つかりません (URI: %s): %w", gcsURI, err)
		}
		return nil, fmt.Errorf("GCSファイルの読み込みに失敗しました (URI: %s): %w", gcsURI, err)
	}
	return rc, nil
}

// openS3Object は、S3 URI からオブジェクトを読み込み、io.ReadCloser を返します。
func (r *UniversalInputReader) openS3Object(ctx context.Context, s3URI string) (io.ReadCloser, error) {
	if r.s3Client == nil {
		return nil, fmt.Errorf("S3クライアントが初期化されていないため、S3オブジェクトを読み込めません (URI: %s)", s3URI)
	}

	// URIのパースロジック (util.go の ParseS3URI を使用)
	bucketName, objectPath, err := ParseS3URI(s3URI)
	if err != nil {
		return nil, fmt.Errorf("S3 URIのパースに失敗しました: %w", err)
	}

	// オブジェクトパスが空の場合はエラー（S3でも通常はオブジェクトパスが必要）
	if objectPath == "" {
		return nil, fmt.Errorf("無効なS3 URI形式: %s (オブジェクト名が空です。必須形式: s3://bucket-name/object-name)", s3URI)
	}

	// S3 GetObject APIを呼び出す
	result, err := r.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectPath),
	})

	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, fmt.Errorf("S3オブジェクトが見つかりません (URI: %s): %w", s3URI, err)
		}

		// それ以外の一般的なS3エラー
		return nil, fmt.Errorf("S3ファイルの読み込みに失敗しました (URI: %s): %w", s3URI, err)
	}
	return result.Body, nil
}
