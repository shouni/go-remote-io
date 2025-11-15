package remoteio

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"cloud.google.com/go/storage"
)

const DefaultContentType = "text/plain; charset=utf-8"

// =================================================================
// 1. インターフェース定義
// =================================================================

// GCSOutputWriter は、Google Cloud Storage (GCS) にコンテンツを書き込むためのインターフェースです。
type GCSOutputWriter interface {
	// WriteToGCS は、指定されたバケットとオブジェクトパスに io.Reader からコンテンツを書き込みます。
	WriteToGCS(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error
}

// LocalOutputWriter は、ローカルファイルシステムにコンテンツを書き込むためのインターフェースです。
type LocalOutputWriter interface {
	// WriteToLocal は、指定されたローカルパスに io.Reader からコンテンツを書き込みます。
	WriteToLocal(ctx context.Context, path string, contentReader io.Reader) error
}

// =================================================================
// 2. 具象構造体とコンストラクタ (UniversalIOWriterへ統合)
// =================================================================

// UniversalIOWriter は GCSOutputWriter と LocalOutputWriter の両方を満たす具象型です。
type UniversalIOWriter struct {
	gcsClient *storage.Client
	// LocalFileWriter の機能は外部依存がないため、フィールドは不要
}

// NewUniversalIOWriter は新しい UniversalIOWriter インスタンスを作成します。
// Factoryはこの関数を使って、GCSクライアントを注入したI/Oライターを生成します。
func NewUniversalIOWriter(client *storage.Client) *UniversalIOWriter {
	return &UniversalIOWriter{gcsClient: client}
}

// =================================================================
// 3. コアロジック (実装)
// =================================================================

// WriteToGCS は GCSOutputWriter インターフェースを実装します。
func (w *UniversalIOWriter) WriteToGCS(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error {
	targetURI := fmt.Sprintf("gs://%s/%s", bucketName, objectPath)

	if bucketName == "" {
		return fmt.Errorf("GCSへの書き込みに失敗しました: バケット名が空です")
	}
	if objectPath == "" {
		return fmt.Errorf("GCSへの書き込みに失敗しました: オブジェクトパスが空です")
	}
	if w.gcsClient == nil {
		// このチェックはFactory側でもされるが、堅牢性向上のため
		return fmt.Errorf("GCSへの書き込みに失敗しました: GCSクライアントが初期化されていません")
	}

	slog.Info("GCS書き込み処理開始", slog.String("uri", targetURI), slog.String("content_type", contentType))

	bucket := w.gcsClient.Bucket(bucketName)
	obj := bucket.Object(objectPath)

	wc := obj.NewWriter(ctx)

	if contentType == "" {
		wc.ContentType = DefaultContentType
	} else {
		wc.ContentType = contentType
	}

	if _, err := io.Copy(wc, contentReader); err != nil {
		// Copy失敗時はwriterをクローズし、エラーを返す
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
func (w *UniversalIOWriter) WriteToLocal(ctx context.Context, path string, contentReader io.Reader) error {
	// Contextは、ローカルファイルの操作では通常使用されないが、シグネチャを合わせる
	_ = ctx
	slog.Info("ローカル書き込み処理開始", slog.String("path", path))

	// ★修正適用: 出力先のディレクトリが存在しない場合は作成 (os.MkdirAll)
	outputDir := filepath.Dir(path)
	if outputDir != "" && outputDir != "." {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			slog.Error("出力ディレクトリの作成に失敗", slog.String("path", path), slog.String("error", err.Error()))
			return fmt.Errorf("出力ディレクトリ(%s)の作成に失敗しました: %w", outputDir, err)
		}
	}

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

// 型アサーションチェック (UniversalIOWriterが両方のインターフェースを満たしていることを確認)
var _ GCSOutputWriter = (*UniversalIOWriter)(nil)
var _ LocalOutputWriter = (*UniversalIOWriter)(nil)
