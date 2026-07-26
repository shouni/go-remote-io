package remoteio

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// writeLocal はローカルファイルシステムに書き込みます。
// 注意: ローカルファイルシステムでは ContentType や ContentDisposition などのメタデータは保存されず、無視されます。
func (w *UniversalIOWriter) writeLocal(ctx context.Context, path string, contentReader io.Reader, cfg *writeConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	slog.DebugContext(ctx, "ローカル書き込み処理開始",
		slog.String("path", path),
		slog.String("content_type", cfg.contentType),
		slog.String("disposition", cfg.contentDisposition),
		slog.String("cache_control", cfg.cacheControl),
	)

	outputDir := filepath.Dir(path)
	if outputDir != "" && outputDir != "." {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("出力ディレクトリ(%s)の作成に失敗しました: %w", outputDir, err)
		}
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("ローカルファイル(%s)の作成に失敗しました: %w", path, err)
	}

	if _, err := io.Copy(file, &ctxReader{ctx: ctx, r: contentReader}); err != nil {
		_ = file.Close()
		return fmt.Errorf("ローカルファイル(%s)へのコンテンツ書き込み中にエラーが発生しました: %w", path, err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("ローカルファイル(%s)のクローズに失敗しました: %w", path, err)
	}

	slog.DebugContext(ctx, "ローカル書き込み処理完了", slog.String("path", path))
	return nil
}

func (w *UniversalIOWriter) deleteLocal(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ローカルファイルの削除に失敗しました: %w", err)
	}
	return nil
}
