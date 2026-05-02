package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"cloud.google.com/go/storage"
)

func (w *UniversalIOWriter) writeGCS(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error {
	if bucketName == "" {
		return fmt.Errorf("GCSへの書き込みに失敗しました: バケット名が空です")
	}
	if objectPath == "" {
		return fmt.Errorf("GCSへの書き込みに失敗しました: オブジェクトパスが空です")
	}
	if w.gcsClient == nil {
		return fmt.Errorf("GCSへの書き込みに失敗しました: GCSクライアントが初期化されていません")
	}

	targetURI := BuildGCSURI(bucketName, objectPath)
	slog.Info("GCS書き込み処理開始", slog.String("uri", targetURI), slog.String("content_type", contentType))

	wc := w.gcsClient.Bucket(bucketName).Object(objectPath).NewWriter(ctx)
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

// WriteToGCS は GCS への直接書き込みを行います。
func (w *UniversalIOWriter) WriteToGCS(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, contentType string) error {
	return w.writeGCS(ctx, bucketName, objectPath, contentReader, contentType)
}

func (w *UniversalIOWriter) deleteGCS(ctx context.Context, bucketName, objectPath string) error {
	if w.gcsClient == nil {
		return errors.New("GCSクライアントが未初期化です")
	}
	err := w.gcsClient.Bucket(bucketName).Object(objectPath).Delete(ctx)
	if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return fmt.Errorf("GCSオブジェクトの削除に失敗しました: %w", err)
	}
	return nil
}
