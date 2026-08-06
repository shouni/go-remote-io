package remoteio

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// localHandler はローカルファイルシステムを扱う SchemeHandler です。
// Scheme() が空文字なので、どのスキームにも一致しなかったパスを受け取ります。
type localHandler struct{}

var _ SchemeHandler = localHandler{}

// NewLocalHandler はローカルファイルシステム用のハンドラを返します。
// Router へ渡すとフォールバック（スキームを持たないパスの担当）になります。
func NewLocalHandler() SchemeHandler { return localHandler{} }

// Scheme は空文字を返し、フォールバックであることを示します。
func (localHandler) Scheme() string { return "" }

// Open はローカルファイルを開きます。
// 見つからない場合のエラーは os.ErrNotExist を含むため、呼び出し側は
// リモートと同じく errors.Is で判定できます。
func (localHandler) Open(_ context.Context, path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ローカルファイルのオープンに失敗しました: %w", err)
	}
	return file, nil
}

// List は path 配下のファイルを列挙します。
//
// 区切り文字が指定されていない場合は再帰的に列挙します。GCS / S3 の一覧は
// prefix 配下を再帰的に返すため、ローカルだけ直下で止まると同じ呼び出しが
// スキームによって別の意味になり、呼び出し側からはその違いが見えません。
// 区切り文字を指定したときは prefix 直下のみを対象とし、ディレクトリを
// 区切り文字で終わるパスとして併せて列挙します（疑似ディレクトリ相当）。
func (localHandler) List(_ context.Context, path string, callback func(string) error, settings ListSettings) error {
	if settings.Delimiter != "" {
		return listLocalShallow(path, callback, settings)
	}
	return listLocalRecursive(path, callback)
}

// listLocalShallow は path 直下のみを列挙します。
func listLocalShallow(path string, callback func(string) error, settings ListSettings) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("ローカルディレクトリの読み込みに失敗しました (path: %s): %w", path, err)
	}
	for _, entry := range entries {
		name := filepath.Join(path, entry.Name())
		if entry.IsDir() {
			name += settings.Delimiter
		}
		if err := callback(name); err != nil {
			return err
		}
	}
	return nil
}

// listLocalRecursive は path 配下のファイルを再帰的に列挙します（ディレクトリ自体は返しません）。
func listLocalRecursive(path string, callback func(string) error) error {
	err := filepath.WalkDir(path, func(entryPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		return callback(entryPath)
	})
	if err != nil {
		// callback が返したエラーはそのまま伝えます（walk の失敗と区別できるようにするため）。
		if _, ok := err.(*fs.PathError); ok {
			return fmt.Errorf("ローカルディレクトリの読み込みに失敗しました (path: %s): %w", path, err)
		}
		return err
	}
	return nil
}

// Exists はローカルファイルの存在を確認します。
func (localHandler) Exists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("ローカルファイルのステータス取得に失敗しました: %w", err)
}

// Write はローカルファイルシステムに書き込みます。
// 注意: ローカルファイルシステムでは ContentType や ContentDisposition などの
// メタデータは保存されず、無視されます。
func (localHandler) Write(ctx context.Context, path string, contentReader io.Reader, settings WriteSettings) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	slog.DebugContext(ctx, "ローカル書き込み処理開始",
		slog.String("path", path),
		slog.String("content_type", settings.ContentType),
		slog.String("disposition", settings.ContentDisposition),
		slog.String("cache_control", settings.CacheControl),
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

	// os.File への io.Copy は ctx を見ないため、ctxReader を挟んでキャンセルを検知します。
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

// Delete はローカルファイルを削除します。不在はエラーにしません。
func (localHandler) Delete(_ context.Context, path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ローカルファイルの削除に失敗しました: %w", err)
	}
	return nil
}
