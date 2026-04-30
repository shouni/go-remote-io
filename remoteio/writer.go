package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const DefaultContentType = "text/plain; charset=utf-8"

// =================================================================
// 2. 具象構造体とコンストラクタ (UniversalIOWriterへ統合)
// =================================================================

// UniversalIOWriter は GCSOutputWriter, S3OutputWriter, LocalOutputWriter のすべてを満たす具象型です。
type UniversalIOWriter struct {
	gcsClient *storage.Client
	s3Client  *s3.Client
}

// NewUniversalIOWriter は新しい UniversalIOWriter インスタンスを作成します。
// GCSクライアントとS3クライアントを注入します。
func NewUniversalIOWriter(gcsClient *storage.Client, s3Client *s3.Client) *UniversalIOWriter {
	return &UniversalIOWriter{gcsClient: gcsClient, s3Client: s3Client}
}

// =================================================================
// コアロジック (実装)
// =================================================================

// Write は OutputWriter インターフェースの汎用メソッドを実装します。
// パスのプレフィックスを見て WriteToGCS, WriteToS3, または WriteToLocal へ処理を委譲します。
func (w *UniversalIOWriter) Write(ctx context.Context, path string, contentReader io.Reader, contentType string) error {
	if IsGCSURI(path) {
		bucketName, objectPath, err := ParseRemoteURI(path)
		if err != nil {
			return fmt.Errorf("GCS URIのパースに失敗しました: %w", err)
		}
		return w.WriteToGCS(ctx, bucketName, objectPath, contentReader, contentType)
	}

	if IsS3URI(path) {
		bucketName, objectPath, err := ParseRemoteURI(path)
		if err != nil {
			return fmt.Errorf("S3 URIのパースに失敗しました: %w", err)
		}
		return w.WriteToS3(ctx, bucketName, objectPath, contentReader, contentType)
	}

	// ローカルファイルへの書き込み (contentTypeは無視される)
	return w.WriteToLocal(ctx, path, contentReader)
}

// WriteToGCS は GCSOutputWriter インターフェースを実装します。
func (w *UniversalIOWriter) WriteToGCS(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error {
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

	targetURI := BuildGCSURI(bucketName, objectPath)
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

// WriteToS3 は S3OutputWriter インターフェースを実装します。
func (w *UniversalIOWriter) WriteToS3(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error {
	if bucketName == "" {
		return fmt.Errorf("S3への書き込みに失敗しました: バケット名が空です")
	}
	if objectPath == "" {
		return fmt.Errorf("S3への書き込みに失敗しました: オブジェクトパスが空です")
	}
	if w.s3Client == nil {
		// このチェックはFactory側でもされるが、堅牢性向上のため
		return fmt.Errorf("S3への書き込みに失敗しました: S3クライアントが初期化されていません")
	}

	targetURI := BuildS3URI(bucketName, objectPath)
	slog.Info("S3書き込み処理開始", slog.String("uri", targetURI), slog.String("content_type", contentType))

	// S3 PutObject APIを呼び出す
	_, err := w.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(objectPath),
		Body:        contentReader,
		ContentType: aws.String(contentType),
	})

	if err != nil {
		slog.Error("S3へのコンテンツ書き込み中にエラーが発生", slog.String("uri", targetURI), slog.String("error", err.Error()))
		return fmt.Errorf("S3へのコンテンツ書き込み中にエラーが発生しました: %w", err)
	}

	slog.Info("S3書き込み処理完了", slog.String("uri", targetURI))
	return nil
}

// WriteToLocal は LocalOutputWriter インターフェースを実装します。
func (w *UniversalIOWriter) WriteToLocal(ctx context.Context, path string, contentReader io.Reader) error {
	// Contextは、ローカルファイルの操作では通常使用されないが、シグネチャを合わせる
	_ = ctx
	slog.Info("ローカル書き込み処理開始", slog.String("path", path))

	// 出力先のディレクトリが存在しない場合は作成 (os.MkdirAll)
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

// Delete はパスに応じて適切なストレージからリソースを削除します。
func (w *UniversalIOWriter) Delete(ctx context.Context, path string) error {
	if IsGCSURI(path) {
		bucketName, objectPath, err := ParseRemoteURI(path)
		if err != nil {
			return fmt.Errorf("GCS URIのパース失敗: %w", err)
		}
		return w.deleteGCS(ctx, bucketName, objectPath)
	}

	if IsS3URI(path) {
		bucketName, objectPath, err := ParseRemoteURI(path)
		if err != nil {
			return fmt.Errorf("S3 URIのパース失敗: %w", err)
		}
		return w.deleteS3(ctx, bucketName, objectPath)
	}

	return w.deleteLocal(path)
}

func (w *UniversalIOWriter) deleteGCS(ctx context.Context, bucketName, objectPath string) error {
	if w.gcsClient == nil {
		return errors.New("gcs client is not initialized")
	}
	err := w.gcsClient.Bucket(bucketName).Object(objectPath).Delete(ctx)
	if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return fmt.Errorf("failed to delete GCS object: %w", err)
	}
	return nil
}

func (w *UniversalIOWriter) deleteS3(ctx context.Context, bucketName, objectPath string) error {
	if w.s3Client == nil {
		return errors.New("s3 client is not initialized")
	}
	_, err := w.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectPath),
	})

	var nsk *types.NoSuchKey
	if err != nil && !errors.As(err, &nsk) {
		return fmt.Errorf("failed to delete S3 object: %w", err)
	}
	return nil
}

func (w *UniversalIOWriter) deleteLocal(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete local file: %w", err)
	}
	return nil
}
