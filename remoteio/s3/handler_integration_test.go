package s3_test

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/s3"
)

const testBucket = "test-bucket"

// newTestRouter は、インプロセスの S3 フェイクに接続した Router を返します。
//
// docker も AWS 認証情報も要りません。これが無いと読み書き・一覧・存在確認の実装が
// 1 行も実行されないままになります（CommonPrefixes の扱いなど、壊れても静かな
// ロジックがここにあります）。
func newTestRouter(t *testing.T, objects map[string]string) *remoteio.Router {
	t.Helper()

	backend := s3mem.New()
	faker := gofakes3.New(backend)
	server := httptest.NewServer(faker.Server())
	t.Cleanup(server.Close)

	require.NoError(t, backend.CreateBucket(testBucket))

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion("ap-northeast-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)

	client := awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		// フェイクは仮想ホスト形式のバケット名を解決できないためパス形式で話します。
		o.UsePathStyle = true
	})

	for name, content := range objects {
		_, err := client.PutObject(context.Background(), &awss3.PutObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(name),
			Body:   strings.NewReader(content),
		})
		require.NoError(t, err)
	}

	return remoteio.NewRouter(s3.NewHandler(client), remoteio.NewLocalHandler())
}

func uri(name string) string { return remoteio.BuildS3URI(testBucket, name) }

func TestHandlerOpen(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t, map[string]string{"data/report.txt": "hello s3"})

	t.Run("既存オブジェクトを読める", func(t *testing.T) {
		rc, err := router.Open(ctx, uri("data/report.txt"))
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "hello s3", string(got))
	})

	// GCS 側と同じく os.ErrNotExist で判定できること。
	// これが揃っていて初めてスキーム非依存に書けます。
	t.Run("不在は os.ErrNotExist を包んで返す", func(t *testing.T) {
		_, err := router.Open(ctx, uri("data/missing.txt"))
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("オブジェクト名が空の URI は拒否する", func(t *testing.T) {
		_, err := router.Open(ctx, "s3://"+testBucket)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "オブジェクト名が空です")
	})
}

func TestHandlerExists(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t, map[string]string{"data/report.txt": "x"})

	exists, err := router.Exists(ctx, uri("data/report.txt"))
	require.NoError(t, err)
	assert.True(t, exists)

	// HeadObject は NoSuchKey ではなく NotFound を返すことがあり、
	// 片方しか見ていないと不在が「エラー」になります。
	exists, err = router.Exists(ctx, uri("data/missing.txt"))
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestHandlerWriteAndDelete(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t, nil)
	target := uri("data/new.txt")

	require.NoError(t, router.Write(ctx, target, strings.NewReader("written"),
		remoteio.WithContentType("text/plain"),
		remoteio.WithCacheControl("public, max-age=60"),
		remoteio.WithInline(),
	))

	rc, err := router.Open(ctx, target)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, "written", string(got))

	require.NoError(t, router.Delete(ctx, target))

	exists, err := router.Exists(ctx, target)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestHandlerList(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t, map[string]string{
		"data/README.md":       "a",
		"data/dir-1/a.txt":     "b",
		"data/dir-1/b.txt":     "c",
		"data/dir-2/a.txt":     "d",
		"data-archive/old.txt": "e",
	})

	collect := func(prefix string, opts ...remoteio.ListOption) []string {
		var got []string
		require.NoError(t, router.List(ctx, uri(prefix), func(p string) error {
			got = append(got, p)
			return nil
		}, opts...))
		return got
	}

	// 区切り文字なしの prefix は素の文字列前方一致です（GCS と同じ意味論）。
	t.Run("区切り文字なしは文字列前方一致で再帰的に列挙する", func(t *testing.T) {
		assert.ElementsMatch(t, []string{
			uri("data/README.md"),
			uri("data/dir-1/a.txt"),
			uri("data/dir-1/b.txt"),
			uri("data/dir-2/a.txt"),
			uri("data-archive/old.txt"),
		}, collect("data"))
	})

	// 疑似ディレクトリは Contents ではなく CommonPrefixes に入るため、
	// そこを取り違えると疑似ディレクトリの一覧が丸ごと落ちます。
	t.Run("区切り文字ありでは直下と疑似ディレクトリを返す", func(t *testing.T) {
		assert.ElementsMatch(t, []string{
			uri("data/README.md"),
			uri("data/dir-1/"),
			uri("data/dir-2/"),
		}, collect("data", remoteio.WithDelimiter("/")))
	})

	t.Run("callback のエラーで列挙を打ち切る", func(t *testing.T) {
		err := router.List(ctx, uri("data"), func(string) error { return assert.AnError })
		assert.ErrorIs(t, err, assert.AnError)
	})
}

// 未登録スキームは、対応していないことがそのまま分かるエラーになること。
func TestRouterRejectsUnregisteredScheme(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t, nil)

	_, err := router.Open(ctx, "gs://other-bucket/obj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未対応のURIスキームです")
}
