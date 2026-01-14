package remoteio

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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
// 3. コアロジック (実装)
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
// 4. GCS / S3 内部実装
// =================================================================

// openGCSObject は、GCS URI からオブジェクトを読み込み、io.ReadCloser を返します。
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
		return nil, fmt.Errorf("GCS読み込み失敗: %w", err)
	}
	return rc, nil
}

// listGCSObjects バケットのプレフィックス配下のオブジェクト URI 一覧を取得。URI リストまたはエラーを返す
func (r *UniversalInputReader) listGCSObjects(ctx context.Context, gcsURI string) ([]string, error) {
	if r.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントが未初期化です")
	}
	bucketName, prefix, err := ParseGCSURI(gcsURI)
	if err != nil {
		return nil, err
	}

	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

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
		// プレフィックス自体は除外
		if attrs.Name == prefix {
			continue
		}
		files = append(files, fmt.Sprintf("gs://%s/%s", bucketName, attrs.Name))
	}
	return files, nil
}

// openS3Object は、S3 URI からオブジェクトを読み込み、io.ReadCloser を返します。
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
		return nil, fmt.Errorf("S3読み込み失敗: %w", err)
	}
	return result.Body, nil
}

// listS3Objects バケットのプレフィックス配下にあるオブジェクト URI 一覧を取得。URI リストまたはエラーを返却
func (r *UniversalInputReader) listS3Objects(ctx context.Context, s3URI string) ([]string, error) {
	if r.s3Client == nil {
		return nil, fmt.Errorf("S3クライアントが未初期化です")
	}
	bucketName, prefix, err := ParseS3URI(s3URI)
	if err != nil {
		return nil, err
	}

	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	output, err := r.s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("S3リスト取得失敗: %w", err)
	}

	var files []string
	for _, obj := range output.Contents {
		if *obj.Key == prefix {
			continue
		}
		files = append(files, fmt.Sprintf("s3://%s/%s", bucketName, *obj.Key))
	}
	return files, nil
}
