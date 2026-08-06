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
		ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: name},
		Content:     []byte(content),
	}
}

func uri(name string) string { return remoteio.BuildGCSURI(testBucket, name) }

func TestHandlerOpen(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t, object("music/song.txt", "hello gcs"))

	t.Run("既存オブジェクトを読める", func(t *testing.T) {
		rc, err := router.Open(ctx, uri("music/song.txt"))
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "hello gcs", string(got))
	})

	// スキームに依らず errors.Is(err, os.ErrNotExist) で判定できることが、
	// このライブラリの抽象が成立するための条件です。
	t.Run("不在は os.ErrNotExist を包んで返す", func(t *testing.T) {
		_, err := router.Open(ctx, uri("music/missing.txt"))
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
	router := newTestRouter(t, object("music/song.txt", "x"))

	exists, err := router.Exists(ctx, uri("music/song.txt"))
	require.NoError(t, err)
	assert.True(t, exists)

	// 不在は (false, nil)。エラーにすると呼び出し側が毎回握りつぶすことになります。
	exists, err = router.Exists(ctx, uri("music/missing.txt"))
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestHandlerWriteAndDelete(t *testing.T) {
	ctx := context.Background()
	router := newTestRouter(t)
	target := uri("music/new.txt")

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
		object("music/README.md", "a"),
		object("music/job-1/audio.mp3", "b"),
		object("music/job-1/recipe.json", "c"),
		object("music/job-2/audio.mp3", "d"),
		object("music-archive/old.txt", "e"),
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
	// GCS / S3 の意味論そのままなので、music は music-archive/ にも一致します。
	// ディレクトリとして扱いたい場合は末尾に区切り文字を付けるか WithDelimiter を使います。
	t.Run("区切り文字なしは文字列前方一致で再帰的に列挙する", func(t *testing.T) {
		assert.ElementsMatch(t, []string{
			uri("music/README.md"),
			uri("music/job-1/audio.mp3"),
			uri("music/job-1/recipe.json"),
			uri("music/job-2/audio.mp3"),
			uri("music-archive/old.txt"),
		}, collect("music"))
	})

	t.Run("末尾に区切り文字を付ければディレクトリ相当に絞れる", func(t *testing.T) {
		assert.ElementsMatch(t, []string{
			uri("music/README.md"),
			uri("music/job-1/audio.mp3"),
			uri("music/job-1/recipe.json"),
			uri("music/job-2/audio.mp3"),
		}, collect("music/"))
	})

	// 疑似ディレクトリは attrs.Name が空で attrs.Prefix に入るため、
	// そこを取り違えるとジョブ ID の一覧が丸ごと落ちます。
	t.Run("区切り文字ありでは直下と疑似ディレクトリを返す", func(t *testing.T) {
		assert.ElementsMatch(t, []string{
			uri("music/README.md"),
			uri("music/job-1/"),
			uri("music/job-2/"),
		}, collect("music", remoteio.WithDelimiter("/")))
	})

	// WithDelimiter を使うと ListPrefix が末尾を補うため、music-archive/ は外れます。
	t.Run("区切り文字ありではプレフィックスが正規化される", func(t *testing.T) {
		for _, got := range collect("music", remoteio.WithDelimiter("/")) {
			assert.NotContains(t, got, "music-archive")
		}
	})

	t.Run("callback のエラーで列挙を打ち切る", func(t *testing.T) {
		sentinel := assert.AnError
		err := router.List(ctx, uri("music"), func(string) error { return sentinel })
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
