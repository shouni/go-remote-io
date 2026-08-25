package remoteio

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLocalRouter() *Router { return NewSchemeRouter() }

func TestCopyAndMove(t *testing.T) {
	ctx := context.Background()
	router := newLocalRouter()
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "src.txt")
	require.NoError(t, os.WriteFile(src, []byte("payload"), 0o644))

	t.Run("Copy はコピー元を残す", func(t *testing.T) {
		dst := filepath.Join(tmpDir, "sub", "copied.txt")
		require.NoError(t, Copy(ctx, router, router, src, dst))

		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, "payload", string(got))

		assert.FileExists(t, src)
	})

	t.Run("Copy はコピー元が無ければ os.ErrNotExist", func(t *testing.T) {
		err := Copy(ctx, router, router, filepath.Join(tmpDir, "missing.txt"), filepath.Join(tmpDir, "x.txt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("Move はコピー元を消す", func(t *testing.T) {
		moveSrc := filepath.Join(tmpDir, "move-src.txt")
		require.NoError(t, os.WriteFile(moveSrc, []byte("moved"), 0o644))
		dst := filepath.Join(tmpDir, "moved.txt")

		require.NoError(t, Move(ctx, router, router, moveSrc, dst))

		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, "moved", string(got))
		assert.NoFileExists(t, moveSrc)
	})

	// コピーが失敗した時点で止めるため、コピー元は残ります。
	t.Run("Move はコピーに失敗したらコピー元を消さない", func(t *testing.T) {
		moveSrc := filepath.Join(tmpDir, "keep.txt")
		require.NoError(t, os.WriteFile(moveSrc, []byte("keep"), 0o644))

		err := Move(ctx, router, router, moveSrc, "gs://bucket/unreachable.txt")
		require.Error(t, err)
		assert.FileExists(t, moveSrc)
	})
}

func TestReadAll(t *testing.T) {
	ctx := context.Background()
	router := newLocalRouter()
	tmpDir := t.TempDir()

	path := filepath.Join(tmpDir, "a.txt")
	require.NoError(t, os.WriteFile(path, []byte("all of it"), 0o644))

	got, err := ReadAll(ctx, router, path)
	require.NoError(t, err)
	assert.Equal(t, "all of it", string(got))

	_, err = ReadAll(ctx, router, filepath.Join(tmpDir, "missing.txt"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestFiles(t *testing.T) {
	ctx := context.Background()
	router := newLocalRouter()
	tmpDir := t.TempDir()

	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, name), []byte(name), 0o644))
	}

	t.Run("すべて列挙する", func(t *testing.T) {
		var got []string
		for path, err := range Files(ctx, router, tmpDir) {
			require.NoError(t, err)
			got = append(got, filepath.Base(path))
		}
		assert.ElementsMatch(t, []string{"a.txt", "b.txt", "c.txt"}, got)
	})

	// break で抜けたら List 側も打ち切られること（番兵エラーが漏れないこと）。
	t.Run("break で打ち切れる", func(t *testing.T) {
		count := 0
		for _, err := range Files(ctx, router, tmpDir) {
			require.NoError(t, err)
			count++
			break
		}
		assert.Equal(t, 1, count)
	})

	t.Run("エラーは最後に一度だけ渡る", func(t *testing.T) {
		var errs []error
		var paths []string
		for path, err := range Files(ctx, router, filepath.Join(tmpDir, "missing-dir")) {
			paths = append(paths, path)
			if err != nil {
				errs = append(errs, err)
			}
		}
		require.Len(t, errs, 1)
		assert.ErrorIs(t, errs[0], os.ErrNotExist)
		assert.Equal(t, []string{""}, paths)
	})
}

func TestStatHelper(t *testing.T) {
	ctx := context.Background()
	router := newLocalRouter()
	tmpDir := t.TempDir()

	path := filepath.Join(tmpDir, "a.txt")
	require.NoError(t, os.WriteFile(path, []byte("12345"), 0o644))

	t.Run("Stater を実装していれば取得できる", func(t *testing.T) {
		info, err := Stat(ctx, router, path)
		require.NoError(t, err)
		assert.Equal(t, path, info.Path)
		assert.Equal(t, int64(5), info.Size)
		assert.False(t, info.ModTime.IsZero())
		// ローカルファイルシステムは Content-Type を保持しません。
		assert.Empty(t, info.ContentType)
	})

	t.Run("不在は os.ErrNotExist", func(t *testing.T) {
		_, err := Stat(ctx, router, filepath.Join(tmpDir, "missing.txt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	// InputReader に Stater を含めていないため、対応していない実装もあり得ます。
	t.Run("非対応の Reader は型が分かるエラー", func(t *testing.T) {
		_, err := Stat(ctx, readerWithoutStat{}, path)
		assert.ErrorContains(t, err, "メタデータ取得に対応していません")
	})
}

type readerWithoutStat struct{}

func (readerWithoutStat) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("未実装")
}
