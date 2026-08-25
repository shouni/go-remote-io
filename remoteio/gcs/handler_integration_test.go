package gcs_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
)

const testBucket = "test-bucket"

// newTestRouter は、インプロセスの GCS フェイクに接続した Router を返します。
//
// エミュレータをプロセス内で動かすため、docker も認証情報も要りません。これが無いと
// 読み書き・一覧・存在確認の実装（このパッケージの大半）が 1 行も実行されないまま
// になります。以前はここが未検証で、疑似ディレクトリの扱いのように壊れても静かな
// ロジックがテストの外にありました。
func newTestRouter(t *testing.T, objects ...fakestorage.Object) *remoteio.Router {
	t.Helper()

	server, err := fakestorage.NewServerWithOptions(fakestorage.Options{
		InitialObjects:  objects,
		BucketsLocation: "US",
	})
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	server.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: testBucket})

	return remoteio.NewRouter(gcs.NewHandler(server.Client()), remoteio.NewLocalHandler())
}

func object(name, content string) fakestorage.Object {
	return fakestorage.Object{
		BucketName: testBucket, Name: name,
		Content: []byte(content),
	}
}

func uri(name string) string { return remoteio.BuildGCSURI(testBucket, name) }

func TestHandlerOpen(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t, object("data/report.txt", "hello gcs"))

	t.Run("既存オブジェクトを読める", func(t *testing.T) {
		rc, err := router.Open(ctx, uri("data/report.txt"))
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "hello gcs", string(got))
	})

	// スキームに依らず errors.Is(err, os.ErrNotExist) で判定できることが、
	// このライブラリの抽象が成立するための条件です。
	t.Run("不在は os.ErrNotExist を包んで返す", func(t *testing.T) {
		_, err := router.Open(ctx, uri("data/missing.txt"))
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("オブジェクト名が空の URI は拒否する", func(t *testing.T) {
		_, err := router.Open(ctx, "gs://"+testBucket)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "オブジェクト名が空です")
	})
}

func TestHandlerExists(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t, object("data/report.txt", "x"))

	exists, err := router.Exists(ctx, uri("data/report.txt"))
	require.NoError(t, err)
	assert.True(t, exists)

	// 不在は (false, nil)。エラーにすると呼び出し側が毎回握りつぶすことになります。
	exists, err = router.Exists(ctx, uri("data/missing.txt"))
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestHandlerWriteAndDelete(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t)
	target := uri("data/new.txt")

	require.NoError(t, router.Write(ctx, target, strings.NewReader("written"),
		remoteio.WithContentType("text/plain"),
		remoteio.WithCacheControl("public, max-age=60"),
		remoteio.WithAttachment("new.txt"),
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

	// 削除は冪等。不在をエラーにするとリトライのたびに失敗します。
	assert.NoError(t, router.Delete(ctx, target))
}

func TestHandlerList(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t,
		object("data/README.md", "a"),
		object("data/dir-1/a.txt", "b"),
		object("data/dir-1/b.txt", "c"),
		object("data/dir-2/a.txt", "d"),
		object("data-archive/old.txt", "e"),
	)

	collect := func(prefix string, opts ...remoteio.ListOption) []string {
		var got []string
		require.NoError(t, router.List(ctx, uri(prefix), func(p string) error {
			got = append(got, p)
			return nil
		}, opts...))
		return got
	}

	// 区切り文字なしの prefix は「ディレクトリ」ではなく素の文字列前方一致です。
	// GCS / S3 の意味論そのままなので、data は data-archive/ にも一致します。
	// ディレクトリとして扱いたい場合は末尾に区切り文字を付けるか WithDelimiter を使います。
	t.Run("区切り文字なしは文字列前方一致で再帰的に列挙する", func(t *testing.T) {
		assert.ElementsMatch(t, []string{
			uri("data/README.md"),
			uri("data/dir-1/a.txt"),
			uri("data/dir-1/b.txt"),
			uri("data/dir-2/a.txt"),
			uri("data-archive/old.txt"),
		}, collect("data"))
	})

	t.Run("末尾に区切り文字を付ければディレクトリ相当に絞れる", func(t *testing.T) {
		assert.ElementsMatch(t, []string{
			uri("data/README.md"),
			uri("data/dir-1/a.txt"),
			uri("data/dir-1/b.txt"),
			uri("data/dir-2/a.txt"),
		}, collect("data/"))
	})

	// 疑似ディレクトリは attrs.Name が空で attrs.Prefix に入るため、
	// そこを取り違えると疑似ディレクトリの一覧が丸ごと落ちます。
	t.Run("区切り文字ありでは直下と疑似ディレクトリを返す", func(t *testing.T) {
		assert.ElementsMatch(t, []string{
			uri("data/README.md"),
			uri("data/dir-1/"),
			uri("data/dir-2/"),
		}, collect("data", remoteio.WithDelimiter("/")))
	})

	// WithDelimiter を使うと ListPrefix が末尾を補うため、data-archive/ は外れます。
	t.Run("区切り文字ありではプレフィックスが正規化される", func(t *testing.T) {
		for _, got := range collect("data", remoteio.WithDelimiter("/")) {
			assert.NotContains(t, got, "data-archive")
		}
	})

	t.Run("callback のエラーで列挙を打ち切る", func(t *testing.T) {
		sentinel := assert.AnError
		err := router.List(ctx, uri("data"), func(string) error { return sentinel })
		assert.ErrorIs(t, err, sentinel)
	})
}

// 未登録スキームは、対応していないことがそのまま分かるエラーになること。
func TestRouterRejectsUnregisteredScheme(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t)

	_, err := router.Open(ctx, "s3://other-bucket/obj")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未対応のURIスキームです")
}
