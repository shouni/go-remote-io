package remoteio

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"cloud.google.com/go/storage"
)

const DefaultContentType = "text/plain; charset=utf-8"

// =================================================================
// 1. インターフェース定義
// =================================================================

// GCSOutputWriter は、Google Cloud Storage (GCS) にコンテンツを書き込むためのインターフェースです。
type GCSOutputWriter interface {
	// WriteToGCS は、指定されたバケットとオブジェクトパスに io.Reader からコンテンツを書き込みます。
	// contentType は書き込むコンテンツのMIMEタイプを指定します。空文字列の場合、デフォルト値が適用されます。
	WriteToGCS(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error
}

// LocalOutputWriter は、ローカルファイルシステムにコンテンツを書き込むためのインターフェースです。
// このインターフェースは、GCS以外の出力先を抽象化するために導入されました。
type LocalOutputWriter interface {
	// WriteToLocal は、指定されたローカルパスに io.Reader からコンテンツを書き込みます。
	WriteToLocal(ctx context.Context, path string, contentReader io.Reader) error
}

// =================================================================
// 2. 具象構造体とコンストラクタ
// =================================================================

// GCSFileWriter は GCSOutputWriter インターフェースの具象実装です。
type GCSFileWriter struct {
	client *storage.Client
}

// NewGCSFileWriter は新しい GCSFileWriter インスタンスを作成します。
func NewGCSFileWriter(client *storage.Client) *GCSFileWriter {
	return &GCSFileWriter{client: client}
}

// LocalFileWriter は LocalOutputWriter インターフェースの具象実装です。
type LocalFileWriter struct{}

// NewLocalFileWriter は新しい LocalFileWriter インスタンスを作成します。
func NewLocalFileWriter() *LocalFileWriter {
	return &LocalFileWriter{}
}

// =================================================================
// 3. コアロジック (実装)
// =================================================================

// WriteToGCS は GCSOutputWriter インターフェースを実装します。
func (w *GCSFileWriter) WriteToGCS(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error {
	targetURI := fmt.Sprintf("gs://%s/%s", bucketName, objectPath)

	if bucketName == "" {
		return fmt.Errorf("GCSへの書き込みに失敗しました: バケット名が空です")
	}
	if objectPath == "" {
		return fmt.Errorf("GCSへの書き込みに失敗しました: オブジェクトパスが空です")
	}

	slog.Info("GCS書き込み処理開始", slog.String("uri", targetURI), slog.String("content_type", contentType))

	bucket := w.client.Bucket(bucketName)
	obj := bucket.Object(objectPath)

	// GCS WriterはContextをサポート
	wc := obj.NewWriter(ctx)

	if contentType == "" {
		wc.ContentType = DefaultContentType
	} else {
		wc.ContentType = contentType
	}

	if _, err := io.Copy(wc, contentReader); err != nil {
		wc.Close()
		slog.Error("GCSへのコンテンツ書き込み中にエラーが発生", slog.String("uri", targetURI), slog.String("error", err.Error()))
		return fmt.Errorf("GCSへのコンテンツ書き込み中にエラーが発生しました: %w", err)
	}

	if err := wc.Close(); err != nil {
		slog.Error("GCS Writerのクローズに失敗", slog.String("uri", targetURI), slog.String("error", err.Error()))
		return fmt.Errorf("GCS Writerのクローズに失敗しました (アップロード処理中のエラー): %w", err)
	}

	slog.Info("GCS書き込み処理完了", slog.String("uri", targetURI))
	return nil
}

// WriteToLocal は LocalOutputWriter インターフェースを実装します。
func (w *LocalFileWriter) WriteToLocal(ctx context.Context, path string, contentReader io.Reader) error {

	slog.Info("ローカル書き込み処理開始", slog.String("path", path))

	file, err := os.Create(path)
	if err != nil {
		slog.Error("ローカルファイルの作成に失敗", slog.String("path", path), slog.String("error", err.Error()))
		return fmt.Errorf("ローカルファイル(%s)の作成に失敗しました: %w", path, err)
	}
	defer file.Close()

	if _, err := io.Copy(file, contentReader); err != nil {
		slog.Error("ローカルファイルへのコンテンツ書き込み中にエラーが発生", slog.String("path", path), slog.String("error", err.Error()))
		return fmt.Errorf("ローカルファイル(%s)へのコンテンツ書き込み中にエラーが発生しました: %w", path, err)
	}

	slog.Info("ローカル書き込み処理完了", slog.String("path", path))
	return nil
}

// 型アサーションチェック
var _ GCSOutputWriter = (*GCSFileWriter)(nil)
var _ LocalOutputWriter = (*LocalFileWriter)(nil)
