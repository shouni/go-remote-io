package remoteio_test

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
	"github.com/shouni/go-remote-io/remoteio/s3"
)

const multiBucket = "multi-bucket"

// newMultiBundle は、GCS と S3 のフェイクを 1 つの Bundle に束ねて返します。
//
// クラウド SDK を import するのは外部テストパッケージ (remoteio_test) だけです。
// remoteio 本体の依存には入らないため、抽象だけを使うアプリケーションのビルドに
// クラウド SDK は持ち込まれません。
func newMultiBundle(t *testing.T) *remoteio.Bundle {
	t.Helper()
	ctx := context.Background()

	gcsServer, err := fakestorage.NewServerWithOptions(fakestorage.Options{BucketsLocation: "US"})
	require.NoError(t, err)
	t.Cleanup(gcsServer.Stop)
	gcsServer.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: multiBucket})

	s3Backend := s3mem.New()
	s3Server := httptest.NewServer(gofakes3.New(s3Backend).Server())
	t.Cleanup(s3Server.Close)
	require.NoError(t, s3Backend.CreateBucket(multiBucket))

	gcsFactory, err := gcs.New(ctx, gcs.WithClient(gcsServer.Client()))
	require.NoError(t, err)

	s3Factory, err := s3.New(ctx,
		s3.WithEndpoint(s3Server.URL),
		s3.WithPathStyle(),
		s3.WithRegion("ap-northeast-1"),
		s3.WithConfigOptions(awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("test", "test", ""),
		)),
	)
	require.NoError(t, err)

	multi, err := remoteio.NewMultiFactory(gcsFactory, s3Factory)
	require.NoError(t, err)

	bundle, err := remoteio.NewBundle(multi)
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Close() })

	return bundle
}

// gs:// と s3:// を 1 つの Bundle で扱えること。
// Router 自体は元からスキーム非依存でしたが、IOFactory / Bundle の粒度で
// 同じことをする手段がありませんでした。
func TestMultiFactoryHandlesBothClouds(t *testing.T) {
	ctx := context.Background()
	bundle := newMultiBundle(t)
	tmpDir := t.TempDir()

	targets := map[string]string{
		"GCS":         remoteio.BuildGCSURI(multiBucket, "shared/a.txt"),
		"S3":          remoteio.BuildS3URI(multiBucket, "shared/a.txt"),
		"ローカル":        filepath.Join(tmpDir, "a.txt"),
		"file:// URI": remoteio.PrefixFile + filepath.ToSlash(filepath.Join(tmpDir, "b.txt")),
	}

	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			content := "content of " + name
			require.NoError(t, bundle.Writer.Write(ctx, target, strings.NewReader(content)))

			exists, err := bundle.Reader.Exists(ctx, target)
			require.NoError(t, err)
			assert.True(t, exists)

			rc, err := bundle.Reader.Open(ctx, target)
			require.NoError(t, err)
			defer func() { _ = rc.Close() }()

			got, err := io.ReadAll(rc)
			require.NoError(t, err)
			assert.Equal(t, content, string(got))

			require.NoError(t, bundle.Writer.Delete(ctx, target))
			_, err = bundle.Reader.Open(ctx, target)
			assert.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

// 束ねていないスキームは、対応していないことがそのまま分かるエラーになること。
func TestMultiFactoryRejectsUnregisteredScheme(t *testing.T) {
	bundle := newMultiBundle(t)

	_, err := bundle.Reader.Open(context.Background(), "ftp://host/file")
	assert.ErrorContains(t, err, "未対応のURIスキームです")
}

// 署名器はスキームごとに振り分けられること。
// 各署名器は自分のスキーム以外を明確に拒否するため、束ねる側で振り分けないと
// 片方しか使えません。
func TestMultiFactorySignerRoutesByScheme(t *testing.T) {
	ctx := context.Background()
	bundle := newMultiBundle(t)

	t.Run("s3:// は S3 の署名器へ渡る", func(t *testing.T) {
		url, err := bundle.Signer.GenerateSignedURL(ctx, remoteio.BuildS3URI(multiBucket, "a.txt"), "GET", time.Minute)
		require.NoError(t, err)
		assert.Contains(t, url, "X-Amz-Signature")
	})

	t.Run("未登録スキームは拒否される", func(t *testing.T) {
		_, err := bundle.Signer.GenerateSignedURL(ctx, "/local/a.txt", "GET", time.Minute)
		assert.ErrorContains(t, err, "署名付きURLに対応していないスキームです")
	})
}

// Close は束ねた全ファクトリを解放し、冪等であること。
func TestMultiFactoryClose(t *testing.T) {
	ctx := context.Background()

	gcsServer, err := fakestorage.NewServerWithOptions(fakestorage.Options{BucketsLocation: "US"})
	require.NoError(t, err)
	t.Cleanup(gcsServer.Stop)

	gcsFactory, err := gcs.New(ctx, gcs.WithClient(gcsServer.Client()))
	require.NoError(t, err)

	multi, err := remoteio.NewMultiFactory(gcsFactory)
	require.NoError(t, err)

	require.NoError(t, multi.Close())
	assert.NoError(t, multi.Close(), "Close は冪等であるべきです")

	_, err = multi.InputReader()
	assert.Error(t, err)
	_, err = multi.OutputWriter()
	assert.Error(t, err)
	_, err = multi.URLSigner()
	assert.Error(t, err)
}

// SchemeHandler を提供しないファクトリは束ねられないこと。
func TestMultiFactoryRequiresHandlerProvider(t *testing.T) {
	// bundle_test.go の stubFactory は HandlerProvider を実装していません。
	_, err := remoteio.NewMultiFactory(&stubFactory{})
	assert.ErrorContains(t, err, "SchemeHandler を提供しない")

	_, err = remoteio.NewMultiFactory()
	assert.ErrorContains(t, err, "IOFactory が 1 つも指定されていません")
}
