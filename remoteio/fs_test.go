package remoteio

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSAdapter(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := NewStore().Sub(dir)

	require.NoError(t, WriteAll(ctx, store, "a.txt", []byte("alpha")))
	require.NoError(t, WriteAll(ctx, store, "sub/b.txt", []byte("beta")))

	fsys := FS(ctx, store)

	t.Run("fstest.TestFS を通る", func(t *testing.T) {
		// 標準のスイートに通すことで、io/fs の規約（名前の検証、ReadDir の並び、
		// Stat と Open の整合）を自前のアサーションで書き直さずに済みます。
		assert.NoError(t, fstest.TestFS(fsys, "a.txt", "sub/b.txt"))
	})

	t.Run("fs.ReadFile で読める", func(t *testing.T) {
		data, err := fs.ReadFile(fsys, "a.txt")
		require.NoError(t, err)
		assert.Equal(t, "alpha", string(data))
	})

	t.Run("ReadDir はディレクトリを IsDir で返す", func(t *testing.T) {
		entries, err := fs.ReadDir(fsys, ".")
		require.NoError(t, err)

		got := map[string]bool{}
		for _, e := range entries {
			got[e.Name()] = e.IsDir()
		}
		assert.Equal(t, map[string]bool{"a.txt": false, "sub": true}, got)
	})

	t.Run("fs.WalkDir で再帰的に辿れる", func(t *testing.T) {
		var files []string
		require.NoError(t, fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				files = append(files, p)
			}
			return nil
		}))
		assert.ElementsMatch(t, []string{"a.txt", "sub/b.txt"}, files)
	})

	t.Run("不正な名前は fs.ErrInvalid", func(t *testing.T) {
		_, err := fsys.Open("/absolute")
		assert.ErrorIs(t, err, fs.ErrInvalid)

		_, err = fsys.Open("../escape")
		assert.ErrorIs(t, err, fs.ErrInvalid)
	})

	t.Run("不在は fs.ErrNotExist", func(t *testing.T) {
		_, err := fsys.Open("missing.txt")
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("ローカルの実体と一致する", func(t *testing.T) {
		data, err := fs.ReadFile(fsys, "sub/b.txt")
		require.NoError(t, err)

		onDisk, err := fs.ReadFile(FS(ctx, NewStore().Sub(filepath.Join(dir, "sub"))), "b.txt")
		require.NoError(t, err)
		assert.Equal(t, string(onDisk), string(data))
	})
}
