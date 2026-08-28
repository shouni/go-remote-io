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

// newTestFactory は、フェイクのクライアントを注入したファクトリを返します。
// WithClient を通るため、認証情報の無い環境でも走ります。
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

	t.Run("Store は gs:// とローカルを扱う", func(t *testing.T) {
		store, err := factory.Store()
		require.NoError(t, err)

		require.NoError(t, remoteio.WriteAll(ctx, store, uri("a.txt"), []byte("x")))

		dir := t.TempDir()
		require.NoError(t, remoteio.WriteAll(ctx, store, dir+"/local.txt", []byte("y")))

		_, err = store.Open(ctx, "s3://other/key")
		assert.ErrorIs(t, err, remoteio.ErrUnsupportedScheme)
	})

	t.Run("Handler は担当スキームを返す", func(t *testing.T) {
		handler, err := factory.Handler()
		require.NoError(t, err)
		assert.Equal(t, gcs.Scheme, handler.Scheme())
		assert.Equal(t, "gs", handler.Scheme(), "区切りを含まない RFC 3986 の形であること")
	})
}

func TestFactoryClose(t *testing.T) {
	factory := newTestFactory(t)

	t.Run("WithClient のクライアントは閉じない", func(t *testing.T) {
		require.NoError(t, factory.Close())

		// 注入されたクライアントのライフサイクルは呼び出し元に残るため、
		// ファクトリを閉じてもクライアント自体は生きています。
		// ここで確かめるのは、ファクトリが参照を手放したことです。
		_, err := factory.Store()
		assert.ErrorIs(t, err, remoteio.ErrClosed)

		_, err = factory.Handler()
		assert.ErrorIs(t, err, remoteio.ErrClosed)
	})

	t.Run("Close は冪等", func(t *testing.T) {
		assert.NoError(t, factory.Close())
	})
}

// Close と各アクセサが並行に呼ばれても安全であることの確認です。
// クライアントのフィールドを無同期で nil にすると Close と読み出しが競合しますが、
// 並行に触るテストが無いと -race を付けていても検出されません。
func TestFactoryCloseIsRaceFree(t *testing.T) {
	factory := newTestFactory(t)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// クローズ前後どちらでも良く、エラーになること自体は想定内です。
			// ここで見ているのはデータ競合が起きないことです。
			_, _ = factory.Store()
			_, _ = factory.Handler()
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = factory.Close()
	}()
	wg.Wait()
}
