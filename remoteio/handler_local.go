package remoteio

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// localHandler はローカルファイルシステムを扱う Handler です。
// Scheme() が空文字なので、どのスキームにも一致しなかったパスを受け取ります。
type localHandler struct{}

var _ Handler = localHandler{}

// NewLocalHandler はローカルファイルシステム用のハンドラを返します。
// Router へ渡すとフォールバック（スキームを持たないパスの担当）になります。
func NewLocalHandler() Handler { return localHandler{} }

// Scheme は空文字を返し、フォールバックであることを示します。
func (localHandler) Scheme() string { return "" }

// Open はローカルファイルを開きます。
// 見つからない場合のエラーは ErrNotExist を含むため、呼び出し側は
// リモートと同じく errors.Is で判定できます。
//
// ディレクトリは Stat と同じく ErrNotExist を返します。os.Open はディレクトリでも
// 成功し、読もうとした時点で初めて失敗するため、そのままだとローカルだけ
// 「開けるが読めないもの」が存在することになります。リモートには対応する実体が
// 無いので、開く時点で揃えます。
func (localHandler) Open(_ context.Context, path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, wrapf(err, "ローカルファイルのオープンに失敗しました (%s)", path)
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, wrapf(err, "ローカルファイルのステータス取得に失敗しました (%s)", path)
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, wrapf(fs.ErrNotExist, "ディレクトリはオブジェクトではありません (%s)", path)
	}
	return file, nil
}

// Stat はローカルファイルのメタデータを返します。
// ローカルファイルシステムは Content-Type を保持しないため、ContentType は空です。
//
// ディレクトリは ErrNotExist を返します。リモートにディレクトリという実体は無く、
// ここだけ成功すると Exists の意味がスキームによって変わるためです
// （v1 はローカルだけディレクトリに true を返していました）。
// 階層の有無を知りたい場合は List を使ってください。
func (localHandler) Stat(_ context.Context, path string) (ObjectInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ObjectInfo{}, wrapf(err, "ローカルファイルのステータス取得に失敗しました (%s)", path)
	}
	if info.IsDir() {
		return ObjectInfo{}, wrapf(fs.ErrNotExist, "ディレクトリはオブジェクトではありません (%s)", path)
	}
	return ObjectInfo{URI: path, Size: info.Size(), ModTime: info.ModTime()}, nil
}

// List は path 配下を列挙します。
//
// 区切り文字が指定されていない場合は再帰的にファイルだけを列挙します。
// GCS / S3 の一覧はプレフィックス配下を再帰的に返すため、ローカルだけ直下で止まると
// 同じ呼び出しがスキームによって別の意味になり、呼び出し側からはその違いが見えません。
// 区切り文字を指定したときは直下のみを対象とし、ディレクトリを IsPrefix の Entry として
// 併せて返します（疑似ディレクトリ相当）。
func (localHandler) List(ctx context.Context, path string, opts ListOptions) iter.Seq2[Entry, error] {
	if opts.Delimiter != "" {
		return listLocalShallow(ctx, path, opts.Delimiter)
	}
	return listLocalRecursive(ctx, path)
}

// listLocalShallow は path 直下のみを列挙します。
func listLocalShallow(ctx context.Context, path, delimiter string) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) {
		entries, err := os.ReadDir(path)
		if err != nil {
			// 不在のディレクトリは「何も無いプレフィックス」として空を返します。
			// リモートに「存在しないプレフィックス」という状態は無いため、
			// ここでエラーにすると同じ呼び出しがスキームによって別の意味になります。
			// 権限エラーなど、不在以外の失敗はそのまま伝えます。
			if errors.Is(err, fs.ErrNotExist) {
				return
			}
			yield(Entry{}, wrapf(err, "ローカルディレクトリの読み込みに失敗しました (%s)", path))
			return
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				yield(Entry{}, err)
				return
			}

			name := entry.Name()
			full := filepath.Join(path, name)
			if entry.IsDir() {
				if !yield(Entry{URI: full + delimiter, Name: name + delimiter, IsPrefix: true}, nil) {
					return
				}
				continue
			}

			// 一覧のためだけに Stat を増やさないよう、取れなければサイズと時刻は
			// ゼロ値のまま返します。名前が拾えないより有用です。
			var size int64
			var modTime time.Time
			if info, err := entry.Info(); err == nil {
				size, modTime = info.Size(), info.ModTime()
			}
			if !yield(Entry{URI: full, Name: name, Size: size, ModTime: modTime}, nil) {
				return
			}
		}
	}
}

// listLocalRecursive は path 配下のファイルを再帰的に列挙します（ディレクトリ自体は返しません）。
func listLocalRecursive(ctx context.Context, root string) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) {
		// 打ち切りを walk へ伝えるための番兵です。callback が返したエラーと
		// walk 自身の失敗を取り違えないよう、専用の値を使います。
		stop := errors.New("remoteio: stop walk")

		err := filepath.WalkDir(root, func(entryPath string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			name, relErr := filepath.Rel(root, entryPath)
			if relErr != nil {
				// root 配下を歩いている以上ここへは来ませんが、来たときは
				// 相対名を諦めてフルパスを入れます。
				name = entryPath
			}

			var size int64
			var modTime time.Time
			if info, infoErr := d.Info(); infoErr == nil {
				size, modTime = info.Size(), info.ModTime()
			}
			if !yield(Entry{URI: entryPath, Name: filepath.ToSlash(name), Size: size, ModTime: modTime}, nil) {
				return stop
			}
			return nil
		})

		switch {
		case err == nil, errors.Is(err, stop):
			return
		case errors.Is(err, fs.ErrNotExist):
			// listLocalShallow と同じ理由で、不在は空の一覧として扱います。
			return
		case errors.Is(err, fs.ErrPermission):
			yield(Entry{}, wrapf(err, "ローカルディレクトリの読み込みに失敗しました (%s)", root))
		default:
			yield(Entry{}, err)
		}
	}
}

// Write はローカルファイルシステムに書き込みます。
//
// 同じディレクトリの一時ファイルへ書いてから差し替えるため、途中で失敗しても
// 書き込み先は変化しません。os.Create へ直接書くと、ctx のキャンセルや I/O エラーで
// 抜けたときに中途半端なファイルが残ります。
//
// 注意: ContentType や ContentDisposition などのメタデータは保存されず、無視されます。
func (localHandler) Write(ctx context.Context, path string, src io.Reader, opts WriteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	slog.DebugContext(ctx, "ローカル書き込み処理開始",
		slog.String("path", path),
		slog.String("content_type", opts.ContentType),
		slog.String("disposition", opts.ContentDisposition),
		slog.String("cache_control", opts.CacheControl),
	)

	outputDir := filepath.Dir(path)
	if outputDir == "" {
		outputDir = "."
	}
	if outputDir != "." {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return wrapf(err, "出力ディレクトリ(%s)の作成に失敗しました", outputDir)
		}
	}

	tmp, err := os.CreateTemp(outputDir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return wrapf(err, "ローカルファイル(%s)の作成に失敗しました", path)
	}
	tmpName := tmp.Name()
	// 失敗経路では必ず消します。成功時は差し替え済みで Remove は不在エラーになるだけです。
	defer func() { _ = os.Remove(tmpName) }()

	// os.File への io.Copy は ctx を見ないため、ctxReader を挟んでキャンセルを検知します。
	if _, err := io.Copy(tmp, &ctxReader{ctx: ctx, r: src}); err != nil {
		_ = tmp.Close()
		return wrapf(err, "ローカルファイル(%s)へのコンテンツ書き込み中にエラーが発生しました", path)
	}

	// rename の前に内容を確定させます。fsync が無いと、クラッシュ時に
	// 名前だけ差し替わって中身が空のファイルが残り得ます。
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return wrapf(err, "ローカルファイル(%s)の同期に失敗しました", path)
	}

	// CreateTemp は 0600 で作るため、通常の作成に近い権限へ戻します。
	// 既存ファイルを差し替える場合は、その権限を引き継ぎます。
	mode := fs.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		mode = info.Mode().Perm()
	}
	// パスではなく開いているディスクリプタへ適用します（差し替え対象を取り違えないため）。
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return wrapf(err, "ローカルファイル(%s)のパーミッション設定に失敗しました", path)
	}

	if err := tmp.Close(); err != nil {
		return wrapf(err, "ローカルファイル(%s)のクローズに失敗しました", path)
	}

	if opts.IfNotExists {
		// link は対象が既に在ると EEXIST で失敗します。Exists で確かめてから
		// rename する形と違い、確認と作成の間に他のプロセスが割り込めません。
		if err := os.Link(tmpName, path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return wrapf(fs.ErrExist, "ローカルファイル(%s)は既に存在します", path)
			}
			return wrapf(err, "ローカルファイル(%s)の作成に失敗しました", path)
		}
		slog.DebugContext(ctx, "ローカル書き込み処理完了", slog.String("path", path))
		return nil
	}

	if err := os.Rename(tmpName, path); err != nil {
		return wrapf(err, "ローカルファイル(%s)の差し替えに失敗しました", path)
	}

	slog.DebugContext(ctx, "ローカル書き込み処理完了", slog.String("path", path))
	return nil
}

// Delete はローカルファイルを削除します。不在はエラーにしません。
func (localHandler) Delete(_ context.Context, path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return wrapf(err, "ローカルファイルの削除に失敗しました (%s)", path)
	}
	return nil
}
