package remoteio

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
)

// =================================================================
// 1. インターフェース定義
// =================================================================

// GCSOutputWriter は、コンテンツをGoogle Cloud Storageに書き込むための
// インターフェースを定義します。
type GCSOutputWriter interface {
	// WriteToGCS は、指定されたバケットとオブジェクトパスにコンテンツを書き込みます。
	WriteToGCS(ctx context.Context, bucketName, objectPath string, content string) error
}

// =================================================================
// 2. 具象構造体とコンストラクタ
// =================================================================

// GCSFileWriter は GCSOutputWriter インターフェースの具象実装です。
type GCSFileWriter struct {
	client *storage.Client
}

// NewGCSFileWriter は新しい GCSFileWriter インスタンスを作成します。
// 依存関係として GCS クライアントを注入します。
func NewGCSFileWriter(client *storage.Client) *GCSFileWriter {
	return &GCSFileWriter{client: client}
}

// =================================================================
// 3. コアロジック (実装)
// =================================================================

// WriteToGCS は指定されたバケットとパスにコンテンツを書き込みます。
func (w *GCSFileWriter) WriteToGCS(ctx context.Context, bucketName, objectPath string, content string) error {
	// バケットとオブジェクトの参照を取得
	bucket := w.client.Bucket(bucketName)
	obj := bucket.Object(objectPath)

	// Writerを取得し、コンテキストを使用してタイムアウトやキャンセルを処理可能にする
	wc := obj.NewWriter(ctx)

	// MarkdownファイルとしてContent-Typeを明示的に設定
	wc.ContentType = "text/markdown; charset=utf-8"

	// 書き込み
	if _, err := wc.Write([]byte(content)); err != nil {
		wc.Close() // 書き込みエラー時は必ず閉じる
		return fmt.Errorf("GCSへのコンテンツ書き込みに失敗しました: %w", err)
	}

	// Writerを閉じる (これが実際のアップロードをトリガーします)
	if err := wc.Close(); err != nil {
		return fmt.Errorf("GCS Writerのクローズに失敗しました (アップロード失敗): %w", err)
	}

	return nil
}

// 型アサーションチェック: GCSFileWriter が GCSOutputWriter インターフェースを満たしていることを確認
var _ GCSOutputWriter = (*GCSFileWriter)(nil)
