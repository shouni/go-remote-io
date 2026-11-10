package remoteio_test

import (
	"context"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/memio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeletePrefixRemote checks that bulk deletion on a remote scheme works both on the
// root store, given absolute URIs, and on a scoped store narrowed from it. Using
// Entry.URI is ErrAbsoluteName on the scoped store, and using Name is a relative name on
// the root, so the point here is that Join(prefix, Name) is right for both.
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
