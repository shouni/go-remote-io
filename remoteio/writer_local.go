package remoteio

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

func (w *UniversalIOWriter) writeLocal(ctx context.Context, path string, contentReader io.Reader) error {
	_ = ctx
	slog.Info("ローカル書き込み処理開始", slog.String("path", path))

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

// WriteToLocal はローカルファイルへの直接書き込みを行います。
func (w *UniversalIOWriter) WriteToLocal(ctx context.Context, path string, contentReader io.Reader) error {
	return w.writeLocal(ctx, path, contentReader)
}

func (w *UniversalIOWriter) deleteLocal(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ローカルファイルの削除に失敗しました: %w", err)
	}
	return nil
}
