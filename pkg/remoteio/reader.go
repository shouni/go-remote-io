package remoteio

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"cloud.google.com/go/storage"
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

// LocalGCSInputReader は InputReader の具象実装であり、
// ローカルファイルと GCS オブジェクトの読み込みを処理します。
type LocalGCSInputReader struct {
	gcsClient *storage.Client
}

// NewLocalGCSInputReader は LocalGCSInputReader の新しいインスタンスを作成します。
// 依存関係として GCS クライアントを注入します。
func NewLocalGCSInputReader(gcsClient *storage.Client) *LocalGCSInputReader {
	return &LocalGCSInputReader{
		gcsClient: gcsClient,
	}
}

// =================================================================
// 3. コアロジック (実装)
// =================================================================

// Open は、ファイルパスを検査し、ローカルファイルまたはGCSからストリームを開きます。
func (r *LocalGCSInputReader) Open(ctx context.Context, filePath string) (io.ReadCloser, error) {
	// GCS URI 判定ロジック
	if strings.HasPrefix(filePath, "gs://") {
		return r.openGCSObject(ctx, filePath)
	}

	// ローカルファイルパスの処理
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("ローカルファイルのオープンに失敗しました: %w", err)
	}
	return file, nil
}

// openGCSObject は、GCS URI からオブジェクトを読み込み、io.ReadCloser を返します。
func (r *LocalGCSInputReader) openGCSObject(ctx context.Context, gcsURI string) (io.ReadCloser, error) {
	if r.gcsClient == nil {
		return nil, fmt.Errorf("GCS URIが指定されましたが、GCSクライアントが初期化されていません。")
	}

	// URIのパースロジック
	path := gcsURI[5:]                    // "gs://" を削除
	parts := strings.SplitN(path, "/", 2) // バケットとオブジェクトに分割

	// 1. スラッシュの数が不正な場合（例: gs://bucket）
	if len(parts) != 2 {
		return nil, fmt.Errorf("無効なGCS URI形式です: %s (gs://bucket-name/object-name の形式で指定してください。スラッシュの数が不正です)", gcsURI)
	}
	bucketName := parts[0]
	objectName := parts[1]

	// 2. バケット名が空の場合（例: gs:///object）
	if bucketName == "" {
		return nil, fmt.Errorf("無効なGCS URI形式です: %s (バケット名が空です)", gcsURI)
	}

	// 3. オブジェクト名が空の場合（例: gs://bucket/）
	if objectName == "" {
		return nil, fmt.Errorf("無効なGCS URI形式です: %s (オブジェクト名が空です。このInputReaderは単一のGCSオブジェクトの読み込みに特化しており、ディレクトリパスはサポートしていません)", gcsURI)
	}
	// GCS URI パースロジック完了

	// GCS オブジェクトリーダーを作成
	rc, err := r.gcsClient.Bucket(bucketName).Object(objectName).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("GCSファイルの読み込みに失敗しました (URI: %s): %w", gcsURI, err)
	}
	return rc, nil
}
