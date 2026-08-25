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

// nil クライアントでも panic せず、他の操作と同じくエラーで返ること。
// List だけ parseObjectURI を通らないため、以前ここだけガードが抜けていました。
func TestHandlerNilClient(t *testing.T) {
	ctx := context.Background()
	h := gcs.NewHandler(nil)

	t.Run("Open", func(t *testing.T) {
		_, err := h.Open(ctx, uri("a.txt"))
		assert.ErrorContains(t, err, "未初期化")
	})
	t.Run("List", func(t *testing.T) {
		err := h.List(ctx, uri("a"), func(string) error { return nil }, remoteio.NewListSettings())
		assert.ErrorContains(t, err, "未初期化")
	})
	t.Run("Exists", func(t *testing.T) {
		_, err := h.Exists(ctx, uri("a.txt"))
		assert.ErrorContains(t, err, "未初期化")
	})
	t.Run("Write", func(t *testing.T) {
		err := h.Write(ctx, uri("a.txt"), strings.NewReader("x"), remoteio.NewWriteSettings())
		assert.ErrorContains(t, err, "未初期化")
	})
	t.Run("Delete", func(t *testing.T) {
		assert.ErrorContains(t, h.Delete(ctx, uri("a.txt")), "未初期化")
	})
}

// Stat がサイズ・更新時刻・Content-Type を返すこと。
// Exists は真偽しか返さないため、これが無いとスキームごとの API を直接叩くことになります。
func TestHandlerStat(t *testing.T) {
	ctx := context.Background()
	server := newFakeServer(t)
	router := remoteio.NewSchemeRouter(gcs.NewHandler(server.Client()))

	target := uri("stat/report.json")
	require.NoError(t, router.Write(ctx, target, strings.NewReader(`{"a":1}`),
		remoteio.WithContentType("application/json"),
	))

	t.Run("メタデータを返す", func(t *testing.T) {
		info, err := router.Stat(ctx, target)
		require.NoError(t, err)
		assert.Equal(t, target, info.Path)
		assert.Equal(t, int64(len(`{"a":1}`)), info.Size)
		assert.Equal(t, "application/json", info.ContentType)
		assert.False(t, info.ModTime.IsZero())
	})

	// 不在の判定が Open と揃っていることが、スキーム非依存に扱えるための条件です。
	t.Run("不在は os.ErrNotExist を包んで返す", func(t *testing.T) {
		_, err := router.Stat(ctx, uri("stat/missing.json"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("オブジェクト名が空の URI は拒否する", func(t *testing.T) {
		_, err := router.Stat(ctx, remoteio.BuildGCSURI(testBucket, ""))
		assert.ErrorContains(t, err, "オブジェクト名が空です")
	})
}

// WithMetadata がユーザー定義メタデータとして保存されること。
func TestHandlerWriteMetadata(t *testing.T) {
	ctx := context.Background()
	server := newFakeServer(t)
	router := remoteio.NewSchemeRouter(gcs.NewHandler(server.Client()))

	require.NoError(t, router.Write(ctx, uri("meta/a.txt"), strings.NewReader("x"),
		remoteio.WithMetadata(map[string]string{"job-id": "42"}),
	))

	attrs, err := server.Client().Bucket(testBucket).Object("meta/a.txt").Attrs(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"job-id": "42"}, attrs.Metadata)
}
