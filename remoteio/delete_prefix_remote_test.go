package remoteio_test

import (
	"context"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/memio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeletePrefixRemote は、絶対 URI を渡すルートストアと、そこから絞ったスコープ付き
// ストアの両方で、リモートスキームの一括削除が効くことを確かめます。Entry.URI を使うと
// スコープ付きでは ErrAbsoluteName になり、Name を使うとルートでは相対名になるため、
// Join(prefix, Name) がどちらでも正しいことがここの要点です。
func TestDeletePrefixRemote(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := remoteio.NewStore(memio.New(memio.WithScheme("gs")))
	for _, uri := range []string{
		"gs://b/jobs/j1/a.txt", "gs://b/jobs/j1/img/b.png", "gs://b/jobs/j2/c.txt", "gs://b/jobs-old/d.txt",
	} {
		require.NoError(t, remoteio.WriteAll(ctx, store, uri, []byte("x")))
	}

	deleted, err := remoteio.DeletePrefix(ctx, store, "gs://b/jobs/j1/")
	require.NoError(t, err)
	assert.Equal(t, 2, deleted)

	deleted, err = remoteio.DeletePrefix(ctx, store.Sub("gs://b/jobs"), "j2")
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	for uri, want := range map[string]bool{
		"gs://b/jobs/j1/a.txt": false, "gs://b/jobs/j1/img/b.png": false,
		"gs://b/jobs/j2/c.txt": false, "gs://b/jobs-old/d.txt": true,
	} {
		ok, err := store.Exists(ctx, uri)
		require.NoError(t, err)
		assert.Equal(t, want, ok, uri)
	}
}
