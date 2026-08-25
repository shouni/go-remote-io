package gcs_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
)

// newFakeServer はインプロセスの GCS フェイクを起動します。
func newFakeServer(t *testing.T) *fakestorage.Server {
	t.Helper()

	server, err := fakestorage.NewServerWithOptions(fakestorage.Options{BucketsLocation: "US"})
	require.NoError(t, err)
	t.Cleanup(server.Stop)
	server.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: testBucket})

	return server
}

// WithClient でクライアントを注入できること。
// 以前は storage.NewClient(ctx) 決め打ちで ADC が必須だったため、
// ファクトリを使うか自前で Router を組むかの二択でした。
func TestFactoryWithClient(t *testing.T) {
	ctx := context.Background()
	server := newFakeServer(t)

	factory, err := gcs.New(ctx, gcs.WithClient(server.Client()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = factory.Close() })

	writer, err := factory.OutputWriter()
	require.NoError(t, err)
	reader, err := factory.InputReader()
	require.NoError(t, err)

	target := uri("options/hello.txt")
	require.NoError(t, writer.Write(ctx, target, strings.NewReader("via factory")))

	rc, err := reader.Open(ctx, target)
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "via factory", string(got))
}

// 注入したクライアントのライフサイクルは呼び出し元に残ること。
// 閉じる主体が 2 つあると、どちらが所有しているのか呼び出し側から分からなくなります。
func TestFactoryDoesNotCloseInjectedClient(t *testing.T) {
	ctx := context.Background()
	server := newFakeServer(t)
	client := server.Client()

	factory, err := gcs.New(ctx, gcs.WithClient(client))
	require.NoError(t, err)
	require.NoError(t, factory.Close())

	// ファクトリのアクセサは閉じているが、注入元のクライアントはまだ使えること。
	_, err = factory.InputReader()
	assert.Error(t, err)

	handler := gcs.NewHandler(client)
	require.NoError(t, handler.Write(ctx, uri("still-usable.txt"), strings.NewReader("ok"), remoteio.NewWriteSettings()))
}

// ファクトリは SchemeHandler を提供し、MultiFactory から束ねられること。
func TestFactorySchemeHandler(t *testing.T) {
	factory, err := gcs.New(context.Background(), gcs.WithClient(newFakeServer(t).Client()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = factory.Close() })

	handler, err := factory.SchemeHandler()
	require.NoError(t, err)
	assert.Equal(t, remoteio.PrefixGCS, handler.Scheme())
}
