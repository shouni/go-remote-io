package s3_test

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/s3"
)

// newFakeS3 はインプロセスの S3 フェイクを起動し、その URL を返します。
func newFakeS3(t *testing.T) string {
	t.Helper()

	backend := s3mem.New()
	server := httptest.NewServer(gofakes3.New(backend).Server())
	t.Cleanup(server.Close)
	require.NoError(t, backend.CreateBucket(testBucket))

	return server.URL
}

// ファクトリ経由で S3 互換ストレージ (MinIO / R2 / フェイク) に接続できること。
// 以前は config.LoadDefaultConfig(ctx) 決め打ちだったため、エンドポイントを
// 差し替える手段が無く、ファクトリを使うか自前で Router を組むかの二択でした。
func TestFactoryWithEndpoint(t *testing.T) {
	ctx := context.Background()

	factory, err := s3.New(ctx,
		s3.WithEndpoint(newFakeS3(t)),
		s3.WithPathStyle(),
		s3.WithRegion("ap-northeast-1"),
		s3.WithConfigOptions(awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("test", "test", ""),
		)),
	)
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

// ファクトリは SchemeHandler を提供し、MultiFactory から束ねられること。
func TestFactorySchemeHandler(t *testing.T) {
	factory, err := s3.New(context.Background(), s3.WithRegion("ap-northeast-1"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = factory.Close() })

	handler, err := factory.SchemeHandler()
	require.NoError(t, err)
	assert.Equal(t, remoteio.PrefixS3, handler.Scheme())
}

// newRouterForEndpoint は、指定エンドポイントへ接続する Router をファクトリ経由で返します。
func newRouterForEndpoint(t *testing.T, endpoint string) *remoteio.Router {
	t.Helper()

	factory, err := s3.New(context.Background(),
		s3.WithEndpoint(endpoint),
		s3.WithPathStyle(),
		s3.WithRegion("ap-northeast-1"),
		s3.WithConfigOptions(awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("test", "test", ""),
		)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = factory.Close() })

	handler, err := factory.SchemeHandler()
	require.NoError(t, err)

	return remoteio.NewSchemeRouter(handler)
}
