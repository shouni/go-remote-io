package remoteio

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWrapfKeepsSentinelForNamesWithPercent checks that the sentinel survives a name
// containing %. Reinterpreting already formatted context as a format shifts the %w
// pairing, and errors.Is stops working.
func TestWrapfKeepsSentinelForNamesWithPercent(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"missing-100%.txt", "a%20b.txt", "%w.txt", "%!s.txt"} {
		t.Run(name, func(t *testing.T) {
			err := wrapf(ErrNotExist, "%s cannot be opened", name)

			require.ErrorIs(t, err, ErrNotExist)
			assert.Equal(t, name+" cannot be opened: "+ErrNotExist.Error(), err.Error())
		})
	}
}

// TestMissingObjectWithPercentInName checks, as consumers see it, that a missing name
// containing % is treated as "not there".
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

// TestDeletePrefix checks that bulk deletion under a prefix correctly does "list, then
// delete one by one". Delete given a prefix silently succeeds, so without this helper
// every consumer writes the same procedure, and range checks and failure handling drift.
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
		assert.True(t, ok, "the neighbouring job was deleted too")

		// The empty string is the whole scope.
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
