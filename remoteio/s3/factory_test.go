package s3_test

import (
	"context"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/s3"
)

func newTestFactory(t *testing.T) *s3.ClientFactory {
	t.Helper()

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(s3.DefaultRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)

	factory, err := s3.New(context.Background(), s3.WithClient(awss3.NewFromConfig(cfg)))
	require.NoError(t, err)
	return factory
}

func TestFactoryOptions(t *testing.T) {
	ctx := context.Background()

	t.Run("WithConfig と WithRegion で解決できる", func(t *testing.T) {
		// New(ctx) だけだと LoadDefaultConfig が認証情報を要求するため、
		// 解決済みの設定を渡す経路をここで通します。
		factory, err := s3.New(ctx,
			s3.WithConfig(aws.Config{Credentials: credentials.NewStaticCredentialsProvider("k", "s", "")}),
			s3.WithRegion("us-east-1"),
			s3.WithEndpoint("http://127.0.0.1:1"),
			s3.WithPathStyle(),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = factory.Close() })

		handler, err := factory.Handler()
		require.NoError(t, err)
		assert.Equal(t, "s3", handler.Scheme(), "区切りを含まない RFC 3986 の形であること")
	})

	t.Run("リージョン未指定なら既定値", func(t *testing.T) {
		factory, err := s3.New(ctx, s3.WithConfig(aws.Config{}))
		require.NoError(t, err)
		t.Cleanup(func() { _ = factory.Close() })

		_, err = factory.Store()
		require.NoError(t, err)
	})
}

func TestFactoryClose(t *testing.T) {
	factory := newTestFactory(t)

	require.NoError(t, factory.Close())

	_, err := factory.Store()
	assert.ErrorIs(t, err, remoteio.ErrClosed)

	_, err = factory.Handler()
	assert.ErrorIs(t, err, remoteio.ErrClosed)

	assert.NoError(t, factory.Close(), "Close は冪等")
}

// Close と各アクセサが並行に呼ばれても安全であることの確認です。
func TestFactoryCloseIsRaceFree(t *testing.T) {
	factory := newTestFactory(t)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
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
