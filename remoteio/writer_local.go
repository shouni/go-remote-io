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

	slog.Info("ローカル書き込み処理開始",
		slog.String("path", path),
		slog.String("content_type", cfg.contentType),
		slog.String("disposition", cfg.contentDisposition),
		slog.String("cache_control", cfg.cacheControl),
	)

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

	if _, err := io.Copy(file, &ctxReader{ctx: ctx, r: contentReader}); err != nil {
		file.Close()
		slog.Error("ローカルファイルへのコンテンツ書き込み中にエラーが発生", slog.String("path", path), slog.String("error", err.Error()))
		return fmt.Errorf("ローカルファイル(%s)へのコンテンツ書き込み中にエラーが発生しました: %w", path, err)
	}

	if err := file.Close(); err != nil {
		slog.Error("ローカルファイルのクローズに失敗", slog.String("path", path), slog.String("error", err.Error()))
		return fmt.Errorf("ローカルファイル(%s)のクローズに失敗しました: %w", path, err)
	}

	slog.Info("ローカル書き込み処理完了", slog.String("path", path))
	return nil
}

func (w *UniversalIOWriter) deleteLocal(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ローカルファイルの削除に失敗しました: %w", err)
	}
	return nil
}

// ctxReader は io.Copy の途中でも ctx のキャンセルを検知できるようにする io.Reader ラッパーです。
// os.File への io.Copy はデフォルトでは ctx を認識しないため、Read の都度キャンセルを確認します。
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (cr *ctxReader) Read(p []byte) (int, error) {
	if err := cr.ctx.Err(); err != nil {
		return 0, err
	}
	return cr.r.Read(p)
}
