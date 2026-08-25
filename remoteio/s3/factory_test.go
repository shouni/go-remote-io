package s3

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientFactory_New(t *testing.T) {
	f, err := New(context.Background())
	require.NoError(t, err)
	require.NotNil(t, f)
	require.NotNil(t, f.client)
}

func TestClientFactory_DefaultRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	f, err := New(context.Background())
	require.NoError(t, err)
	assert.Equal(t, DefaultRegion, f.awsConfig.Region, "リージョン未設定時はデフォルトリージョンが適用されるべきです")
}

func TestClientFactory_Accessors(t *testing.T) {
	f, err := New(context.Background())
	require.NoError(t, err)

	t.Run("Reader delegates to InputReader", func(t *testing.T) {
		r, err := f.Reader()
		require.NoError(t, err)
		assert.NotNil(t, r)
	})

	t.Run("InputReader succeeds while client is alive", func(t *testing.T) {
		r, err := f.InputReader()
		require.NoError(t, err)
		assert.NotNil(t, r)
	})

	t.Run("Writer delegates to OutputWriter", func(t *testing.T) {
		w, err := f.Writer()
		require.NoError(t, err)
		assert.NotNil(t, w)
	})

	t.Run("OutputWriter succeeds while client is alive", func(t *testing.T) {
		w, err := f.OutputWriter()
		require.NoError(t, err)
		assert.NotNil(t, w)
	})

	t.Run("URLSigner succeeds while client is alive", func(t *testing.T) {
		s, err := f.URLSigner()
		require.NoError(t, err)
		assert.NotNil(t, s)
	})
}

func TestClientFactory_Close(t *testing.T) {
	f, err := New(context.Background())
	require.NoError(t, err)

	require.NoError(t, f.Close())
	assert.Nil(t, f.client)

	t.Run("Close is idempotent", func(t *testing.T) {
		assert.NoError(t, f.Close())
	})

	t.Run("accessors fail after Close", func(t *testing.T) {
		_, err := f.InputReader()
		assert.Error(t, err)

		_, err = f.OutputWriter()
		assert.Error(t, err)

		_, err = f.URLSigner()
		assert.Error(t, err)
	})
}

// リージョンの決定順は 明示指定 > 環境や設定ファイル > DefaultRegion であること。
func TestClientFactory_RegionResolution(t *testing.T) {
	t.Run("WithRegion は環境変数より優先される", func(t *testing.T) {
		t.Setenv("AWS_REGION", "us-east-1")

		f, err := New(context.Background(), WithRegion("eu-west-1"))
		require.NoError(t, err)
		assert.Equal(t, "eu-west-1", f.awsConfig.Region)
	})

	t.Run("WithConfig は LoadDefaultConfig を呼ばない", func(t *testing.T) {
		f, err := New(context.Background(), WithConfig(aws.Config{Region: "us-west-2"}))
		require.NoError(t, err)
		assert.Equal(t, "us-west-2", f.awsConfig.Region)
	})

	t.Run("WithConfig でリージョンが空なら DefaultRegion", func(t *testing.T) {
		f, err := New(context.Background(), WithConfig(aws.Config{}))
		require.NoError(t, err)
		assert.Equal(t, DefaultRegion, f.awsConfig.Region)
	})
}

// WithClient は生成済みクライアントをそのまま使い、設定解決を行わないこと。
func TestClientFactory_WithClient(t *testing.T) {
	client := s3.NewFromConfig(aws.Config{Region: "ap-northeast-1"})

	f, err := New(context.Background(), WithClient(client))
	require.NoError(t, err)
	assert.Same(t, client, f.client)
}
