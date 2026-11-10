package gcs_test

import (
	"context"
	"sync"
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
)

// newTestFactory returns a factory with a fake client injected.
// It goes through WithClient, so it runs even without credentials.
func newTestFactory(t *testing.T) *gcs.ClientFactory {
	t.Helper()

	server, err := fakestorage.NewServerWithOptions(fakestorage.Options{BucketsLocation: "US"})
	require.NoError(t, err)
	t.Cleanup(server.Stop)
	server.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: testBucket})

	factory, err := gcs.New(context.Background(), gcs.WithClient(server.Client()))
	require.NoError(t, err)
	return factory
}

func TestFactoryAccessors(t *testing.T) {
	ctx := context.Background()
	factory := newTestFactory(t)

	t.Run("Store handles gs:// and local paths", func(t *testing.T) {
		store, err := factory.Store()
		require.NoError(t, err)

		require.NoError(t, remoteio.WriteAll(ctx, store, uri("a.txt"), []byte("x")))

		dir := t.TempDir()
		require.NoError(t, remoteio.WriteAll(ctx, store, dir+"/local.txt", []byte("y")))

		_, err = store.Open(ctx, "s3://other/key")
		assert.ErrorIs(t, err, remoteio.ErrUnsupportedScheme)
	})

	t.Run("Handler returns its scheme", func(t *testing.T) {
		handler, err := factory.Handler()
		require.NoError(t, err)
		assert.Equal(t, gcs.Scheme, handler.Scheme())
		assert.Equal(t, "gs", handler.Scheme(), "must be the RFC 3986 form without the separator")
	})
}

func TestFactoryClose(t *testing.T) {
	factory := newTestFactory(t)

	t.Run("does not close a WithClient client", func(t *testing.T) {
		require.NoError(t, factory.Close())

		// The lifecycle of an injected client stays with the caller, so the client
		// itself is still alive after the factory is closed.
		// What is checked here is that the factory dropped its reference.
		_, err := factory.Store()
		assert.ErrorIs(t, err, remoteio.ErrClosed)

		_, err = factory.Handler()
		assert.ErrorIs(t, err, remoteio.ErrClosed)
	})

	t.Run("Close is idempotent", func(t *testing.T) {
		assert.NoError(t, factory.Close())
	})
}

// Checks that Close and each accessor are safe to call concurrently.
// Setting the client field to nil without synchronization races Close against reads,
// but without a test touching them concurrently, -race cannot detect it.
func TestFactoryCloseIsRaceFree(t *testing.T) {
	factory := newTestFactory(t)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			// Before or after Close are both fine, and an error is expected.
			// What is checked here is that no data race occurs.
			_, _ = factory.Store()
			_, _ = factory.Handler()
		})
	}
	wg.Go(func() { _ = factory.Close() })
	wg.Wait()
}
