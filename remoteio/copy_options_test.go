package remoteio_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/memio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCopyHonoursWriteOptions checks that a WriteOption takes effect even for a copy
// within one scheme. CopyTo takes no options, so falling through to a server-side copy
// would silently drop them (WithIfNotExists given, yet the target is overwritten, and so on).
func TestCopyHonoursWriteOptions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := remoteio.NewStore(memio.New(memio.WithScheme("gs")))
	require.NoError(t, remoteio.WriteAll(ctx, store, "gs://b/src.txt", []byte("new")))
	require.NoError(t, remoteio.WriteAll(ctx, store, "gs://b/dst.txt", []byte("existing")))

	// Even within one scheme, WithIfNotExists takes effect (the existing object is not overwritten).
	err := store.Copy(ctx, "gs://b/src.txt", "gs://b/dst.txt", remoteio.WithIfNotExists())
	require.ErrorIs(t, err, remoteio.ErrExist)

	got, err := remoteio.ReadAll(ctx, store, "gs://b/dst.txt")
	require.NoError(t, err)
	assert.Equal(t, "existing", string(got), "a copy that should have been refused overwrote the target")

	// The given Content-Type takes effect too.
	require.NoError(t, store.Copy(ctx, "gs://b/src.txt", "gs://b/typed.txt",
		remoteio.WithContentType("application/json")))
	info, err := store.Stat(ctx, "gs://b/typed.txt")
	require.NoError(t, err)
	assert.Equal(t, "application/json", info.ContentType)
}

// TestCopyCarriesSourceAttributes checks that the Content-Type and metadata not specified
// are carried over from the source. Otherwise only the stream relay path paints over them
// with the default Content-Type, and the same Copy gives different results per scheme pair.
func TestCopyCarriesSourceAttributes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := remoteio.NewStore(
		memio.New(memio.WithScheme("gs")),
		memio.New(memio.WithScheme("s3")),
	)
	require.NoError(t, remoteio.WriteAll(ctx, store, "gs://b/src.png", []byte("img"),
		remoteio.WithContentType("image/png"),
		remoteio.WithMetadata(map[string]string{"origin": "job-1"}),
	))

	for _, dst := range []string{"gs://b/same.png", "s3://b/cross.png"} {
		require.NoError(t, store.Copy(ctx, "gs://b/src.png", dst))

		info, err := store.Stat(ctx, dst)
		require.NoError(t, err, dst)
		assert.Equal(t, "image/png", info.ContentType, dst)
		assert.Equal(t, "job-1", info.Metadata["origin"], dst)
	}

	// An explicit Content-Type wins over the carried-over one.
	require.NoError(t, store.Copy(ctx, "gs://b/src.png", "s3://b/typed.bin",
		remoteio.WithContentType("application/octet-stream")))
	info, err := store.Stat(ctx, "s3://b/typed.bin")
	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", info.ContentType)
}

// TestSubRejectsRelativeSegments checks that a scoped store rejects names containing "..".
// The local Join is filepath.Join, so letting them through would read and write outside
// the scope (the same "can write somewhere else" that ErrAbsoluteName prevents).
func TestSubRejectsRelativeSegments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := remoteio.NewStore(memio.New(memio.WithScheme("gs")))
	dir := t.TempDir()
	require.NoError(t, remoteio.WriteAll(ctx, store, filepath.Join(dir, "secret.txt"), []byte("secret")))

	local := store.Sub(filepath.Join(dir, "jobs"))
	remote := store.Sub("gs://b/jobs")

	for _, name := range []string{"../secret.txt", "a/../../secret.txt", "./a.txt", ".."} {
		_, err := local.Open(ctx, name)
		assert.ErrorIs(t, err, remoteio.ErrInvalidURI, "local Open(%q)", name)

		err = remoteio.WriteAll(ctx, local, name, []byte("x"))
		assert.ErrorIs(t, err, remoteio.ErrInvalidURI, "local WriteAll(%q)", name)

		err = remoteio.WriteAll(ctx, remote, name, []byte("x"))
		assert.ErrorIs(t, err, remoteio.ErrInvalidURI, "remote WriteAll(%q)", name)
	}

	// Nothing was written outside the scope.
	got, err := remoteio.ReadAll(ctx, store, filepath.Join(dir, "secret.txt"))
	require.NoError(t, err)
	assert.Equal(t, "secret", string(got))

	// Ordinary names pass (including names starting with a dot that are not "..", ".").
	require.NoError(t, remoteio.WriteAll(ctx, local, "a/b.txt", []byte("ok")))
	require.NoError(t, remoteio.WriteAll(ctx, local, ".hidden", []byte("ok")))
}
