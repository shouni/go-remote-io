package remoteio

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

// FS は Store を読み取り専用の io/fs.FS として見せるアダプタです。
//
// template.ParseFS / http.FileServer / fstest.TestFS など、標準の io/fs を
// 受け取る仕組みへリモートストレージをそのまま渡せます。
//
// io/fs をこのライブラリのコア抽象にしていないのは、fs.FS の Open が
// context を取らないためです。ネットワーク越しの I/O にキャンセルと期限を
// 渡せないインターフェースは土台にできません。だからここでは ctx を
// 構造体へ持たせています（通常は避ける形ですが、fs.FS を満たす唯一の方法です）。
// 呼び出しごとの期限を効かせたい場合は Store を直接使ってください。
//
// 名前は io/fs の規約に従います（スラッシュ区切り、先頭と末尾にスラッシュを
// 付けない、ルートは "."）。バケットやプレフィックスを含めたい場合は
// Store.Sub でスコープを絞ってから渡します。
func FS(ctx context.Context, s Store) fs.FS {
	return &storeFS{ctx: ctx, store: s}
}

type storeFS struct {
	ctx   context.Context
	store Store
}

var (
	_ fs.FS        = (*storeFS)(nil)
	_ fs.StatFS    = (*storeFS)(nil)
	_ fs.ReadDirFS = (*storeFS)(nil)
)

// name は io/fs の名前を Store が受け取る形へ変換します。
func (f *storeFS) name(op, name string) (string, error) {
	if !fs.ValidPath(name) {
		return "", &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return "", nil
	}
	return name, nil
}

func (f *storeFS) Open(name string) (fs.File, error) {
	resolved, err := f.name("open", name)
	if err != nil {
		return nil, err
	}
	if name == "." {
		return &dirFile{fsys: f, name: name}, nil
	}

	rc, err := f.store.Open(f.ctx, resolved)
	if err != nil {
		// オブジェクトが無くても、その名前の下に何かあれば io/fs から見て
		// ディレクトリです。リモートに実体としてのディレクトリは無いため、
		// 「配下に何かあるか」で判定します。
		if errors.Is(err, ErrNotExist) && f.isDir(resolved) {
			return &dirFile{fsys: f, name: name}, nil
		}
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return &storeFile{fsys: f, name: name, rc: rc}, nil
}

// isDir は、その名前の下に 1 件でも要素があるかを返します。
func (f *storeFS) isDir(resolved string) bool {
	for _, err := range f.store.List(f.ctx, resolved, WithDelimiter("/")) {
		return err == nil
	}
	return false
}

func (f *storeFS) Stat(name string) (fs.FileInfo, error) {
	resolved, err := f.name("stat", name)
	if err != nil {
		return nil, err
	}
	if name == "." {
		return dirInfo(path.Base(name)), nil
	}

	info, err := f.store.Stat(f.ctx, resolved)
	if err != nil {
		if errors.Is(err, ErrNotExist) && f.isDir(resolved) {
			return dirInfo(path.Base(name)), nil
		}
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	return fileInfo{name: path.Base(name), size: info.Size, modTime: info.ModTime}, nil
}

// ReadDir は 1 階層分を列挙します。io/fs の規約どおり名前順に並べて返します。
func (f *storeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	resolved, err := f.name("readdir", name)
	if err != nil {
		return nil, err
	}

	var entries []fs.DirEntry
	for entry, err := range f.store.List(f.ctx, resolved, WithDelimiter("/")) {
		if err != nil {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
		}
		base := strings.TrimSuffix(entry.Name, "/")
		if base == "" {
			continue
		}
		if entry.IsPrefix {
			entries = append(entries, dirEntry{info: dirInfo(base)})
			continue
		}
		entries = append(entries, dirEntry{
			info: fileInfo{name: base, size: entry.Size, modTime: entry.ModTime},
		})
	}

	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, nil
}

// storeFile は Store から開いたストリームを fs.File として見せます。
type storeFile struct {
	fsys *storeFS
	name string
	rc   io.ReadCloser
}

func (f *storeFile) Read(p []byte) (int, error) { return f.rc.Read(p) }
func (f *storeFile) Close() error               { return f.rc.Close() }

// Stat は問い合わせを 1 回増やします。開いた時点でメタデータを取ると、
// 中身を読むだけの利用でも必ず 2 往復することになるため、要求されたときだけ取ります。
func (f *storeFile) Stat() (fs.FileInfo, error) { return f.fsys.Stat(f.name) }

// dirFile は ReadDir だけができるディレクトリのハンドルです。
// 反復の位置を持つのは、fs.ReadDirFile が「読み進める」意味論を要求するためです。
type dirFile struct {
	fsys    *storeFS
	name    string
	entries []fs.DirEntry
	offset  int
	loaded  bool
}

func (d *dirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}
func (d *dirFile) Close() error               { return nil }
func (d *dirFile) Stat() (fs.FileInfo, error) { return dirInfo(path.Base(d.name)), nil }

// ReadDir は io/fs の規約に従います。
// n <= 0 なら残り全部を返し、以降の呼び出しは空スライスと nil を返します。
// n > 0 なら最大 n 件を返し、残りが無ければ io.EOF を返します。
func (d *dirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if !d.loaded {
		entries, err := d.fsys.ReadDir(d.name)
		if err != nil {
			return nil, err
		}
		d.entries, d.loaded = entries, true
	}

	remaining := d.entries[d.offset:]
	if n <= 0 {
		d.offset = len(d.entries)
		return remaining, nil
	}
	if len(remaining) == 0 {
		return nil, io.EOF
	}
	if len(remaining) > n {
		remaining = remaining[:n]
	}
	d.offset += len(remaining)
	return remaining, nil
}

// fileInfo と dirInfo は fs.FileInfo の最小実装です。
// リモートのオブジェクトにパーミッションの概念は無いため、読み取り専用の値を返します。
type fileInfo struct {
	name    string
	size    int64
	modTime time.Time
	dir     bool
}

func dirInfo(name string) fileInfo { return fileInfo{name: name, dir: true} }

func (i fileInfo) Name() string { return i.name }
func (i fileInfo) Size() int64  { return i.size }
func (i fileInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i fileInfo) ModTime() time.Time { return i.modTime }
func (i fileInfo) IsDir() bool        { return i.dir }
func (i fileInfo) Sys() any           { return nil }

type dirEntry struct{ info fileInfo }

func (e dirEntry) Name() string               { return e.info.Name() }
func (e dirEntry) IsDir() bool                { return e.info.IsDir() }
func (e dirEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e dirEntry) Info() (fs.FileInfo, error) { return e.info, nil }
