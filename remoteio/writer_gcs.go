package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"cloud.google.com/go/storage"
)

// writeGCS は、内部的に設定された writeConfig を使用して GCS に書き込みます。
func (w *UniversalIOWriter) writeGCS(ctx context.Context, bucketName, objectPath string, contentReader io.Reader, cfg *writeConfig) error {
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
	slog.DebugContext(ctx, "GCS書き込み処理開始",
		slog.String("uri", targetURI),
		slog.String("content_type", cfg.contentType),
		slog.String("disposition", cfg.contentDisposition),
		slog.String("cache_control", cfg.cacheControl),
	)

	wc := w.gcsClient.Bucket(bucketName).Object(objectPath).NewWriter(ctx)
	wc.ContentType = cfg.contentType

	if cfg.contentDisposition != "" {
		wc.ContentDisposition = cfg.contentDisposition
	}

	if cfg.cacheControl != "" {
		wc.CacheControl = cfg.cacheControl
	}

	if _, err := io.Copy(wc, contentReader); err != nil {
		_ = wc.Close()
		return fmt.Errorf("GCSへのコンテンツ書き込み中にエラーが発生しました (URI: %s): %w", targetURI, err)
	}

	if err := wc.Close(); err != nil {
		return fmt.Errorf("GCS Writerのクローズに失敗しました (URI: %s, アップロード処理中のエラー): %w", targetURI, err)
	}

	slog.DebugContext(ctx, "GCS書き込み処理完了", slog.String("uri", targetURI))
	return nil
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
