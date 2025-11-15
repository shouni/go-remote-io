package remoteio

import (
	"context"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/storage"
)

const DefaultContentType = "text/plain; charset=utf-8"

// =================================================================
// 1. インターフェース定義
// =================================================================

// GCSOutputWriter は、コンテンツをGoogle Cloud Storageに書き込むための
// インターフェースを定義します。
type GCSOutputWriter interface {
	// WriteToGCS は、指定されたバケットとオブジェクトパスに io.Reader からコンテンツを書き込みます。
	// contentType は書き込むコンテンツのMIMEタイプを指定します。
	WriteToGCS(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error
	ParseGCSURI(uri string) (bucketName string, objectPath string, err error)
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
// 3. コアロジック (実装) (修正)
// =================================================================

// WriteToGCS は指定されたバケットとパスにコンテンツを書き込みます。
func (w *GCSFileWriter) WriteToGCS(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error {
	if bucketName == "" {
		return fmt.Errorf("GCSへの書き込みに失敗しました: バケット名が空です")
	}
	if objectPath == "" {
		return fmt.Errorf("GCSへの書き込みに失敗しました: オブジェクトパスが空です")
	}
	// バケットとオブジェクトの参照を取得
	bucket := w.client.Bucket(bucketName)
	obj := bucket.Object(objectPath)

	// Writerを取得し、コンテキストを使用してタイムアウトやキャンセルを処理可能にする
	wc := obj.NewWriter(ctx)

	// Content-Typeを設定。空文字列の場合はデフォルト値を適用
	if contentType == "" {
		wc.ContentType = DefaultContentType
	} else {
		wc.ContentType = contentType
	}
	// MIMEタイプ設定ロジック

	// io.Copy を使用してストリーミング書き込み
	if _, err := io.Copy(wc, contentReader); err != nil {
		wc.Close() // 書き込みエラー時は必ず閉じる
		return fmt.Errorf("GCSへのコンテンツ書き込みに失敗しました: %w", err)
	}

	// Writerを閉じる (これが実際のアップロードをトリガーします)
	if err := wc.Close(); err != nil {
		return fmt.Errorf("GCS Writerのクローズに失敗しました (アップロード失敗): %w", err)
	}

	return nil
}

// ParseGCSURI は、指定されたgs://URIをバケット名とオブジェクトパスにパースします。
// URIが "gs://" で始まっていない場合、または形式が正しくない場合はエラーを返します。
func (w *GCSFileWriter) ParseGCSURI(uri string) (bucketName string, objectPath string, err error) {
	// 1. プレフィックスのチェック
	if !strings.HasPrefix(uri, "gs://") {
		return "", "", fmt.Errorf("無効なGCS URI形式: 'gs://'で始まる必要があります")
	}

	// 2. "gs://" を除去
	path := uri[5:]

	// 3. 最初の '/' でバケット名とオブジェクトパスに分割
	idx := strings.Index(path, "/")
	if idx == -1 {
		// "gs://bucket" のようにオブジェクトパスがない場合
		return path, "", nil
	}

	bucketName = path[:idx]
	objectPath = path[idx+1:] // "/" の次から最後まで

	if bucketName == "" {
		return "", "", fmt.Errorf("GCS URIのバケット名が空です: %s", uri)
	}

	return bucketName, objectPath, nil
}

// 型アサーションチェック: GCSFileWriter が GCSOutputWriter インターフェースを満たしていることを確認
var _ GCSOutputWriter = (*GCSFileWriter)(nil)
