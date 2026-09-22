package remoteio

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWrapfKeepsSentinelForNamesWithPercent は、名前に % が入っても番兵が保たれることを
// 検証します。整形済みの文脈を書式として再解釈すると %w の対応がずれ、errors.Is が
// 効かなくなります。
func TestWrapfKeepsSentinelForNamesWithPercent(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"missing-100%.txt", "a%20b.txt", "%w.txt", "%!s.txt"} {
		t.Run(name, func(t *testing.T) {
			err := wrapf(ErrNotExist, "%s を開けません", name)

			require.ErrorIs(t, err, ErrNotExist)
			assert.Equal(t, name+" を開けません: "+ErrNotExist.Error(), err.Error())
		})
	}
}

// TestMissingObjectWithPercentInName は、% を含む名前の不在が「無い」として扱われることを
// 利用者から見える形で確かめます。
func TestMissingObjectWithPercentInName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewStore()
	path := filepath.Join(t.TempDir(), "missing-100%.txt")

	ok, err := store.Exists(ctx, path)
	require.NoError(t, err)
	assert.False(t, ok)

	_, err = store.Open(ctx, path)
	require.ErrorIs(t, err, ErrNotExist)

	_, err = ReadAll(ctx, store, path)
	require.ErrorIs(t, err, ErrNotExist)
}

// TestDeletePrefix は、プレフィックス配下の一括削除が「一覧して 1 つずつ消す」を
// 正しく行うことを検証します。Delete にプレフィックスを渡しても黙って成功するため、
// この補助が無いと各利用者が同じ手順を書き、範囲の検査や失敗の扱いがずれていきます。
func TestDeletePrefix(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	write := func(t *testing.T, store Store, name string) {
		t.Helper()
		require.NoError(t, WriteAll(ctx, store, name, []byte("x")))
	}

	t.Run("配下を再帰的に消し、隣のプレフィックスは残す", func(t *testing.T) {
		t.Parallel()
		store := NewStore()
		dir := t.TempDir()
		write(t, store, filepath.Join(dir, "jobs", "a.txt"))
		write(t, store, filepath.Join(dir, "jobs", "sub", "b.txt"))
		write(t, store, filepath.Join(dir, "jobs-archive", "c.txt"))

		deleted, err := DeletePrefix(ctx, store, filepath.Join(dir, "jobs"))
		require.NoError(t, err)
		assert.Equal(t, 2, deleted)

		for name, want := range map[string]bool{
			filepath.Join(dir, "jobs", "a.txt"):         false,
			filepath.Join(dir, "jobs", "sub", "b.txt"):  false,
			filepath.Join(dir, "jobs-archive", "c.txt"): true,
		} {
			ok, err := store.Exists(ctx, name)
			require.NoError(t, err)
			assert.Equal(t, want, ok, name)
		}
	})

	t.Run("スコープ付きストアには相対名で消せる", func(t *testing.T) {
		t.Parallel()
		store := NewStore()
		dir := t.TempDir()
		write(t, store, filepath.Join(dir, "jobs", "j1", "a.txt"))
		write(t, store, filepath.Join(dir, "jobs", "j2", "b.txt"))

		jobs := store.Sub(filepath.Join(dir, "jobs"))
		deleted, err := DeletePrefix(ctx, jobs, "j1")
		require.NoError(t, err)
		assert.Equal(t, 1, deleted)

		ok, err := jobs.Exists(ctx, "j2/b.txt")
		require.NoError(t, err)
		assert.True(t, ok, "隣のジョブまで消えています")

		// 空文字はスコープ全体。
		deleted, err = DeletePrefix(ctx, jobs, "")
		require.NoError(t, err)
		assert.Equal(t, 1, deleted)
	})

	t.Run("何も無いプレフィックスは 0 件で成功", func(t *testing.T) {
		t.Parallel()
		deleted, err := DeletePrefix(ctx, NewStore(), filepath.Join(t.TempDir(), "missing"))
		require.NoError(t, err)
		assert.Zero(t, deleted)
	})

	t.Run("バケットの根は拒否する", func(t *testing.T) {
		t.Parallel()
		for _, prefix := range []string{"gs://bucket", "gs://bucket/", "s3://bucket"} {
			_, err := DeletePrefix(ctx, NewStore(), prefix)
			require.ErrorIs(t, err, ErrInvalidURI, prefix)
		}
	})
}
