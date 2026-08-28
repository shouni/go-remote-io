package gcs_test

import (
	"context"
	"errors"
	"io"
	"iter"
	"strings"
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
)

const testBucket = "test-bucket"

// newTestStore は、インプロセスの GCS フェイクに接続した Store を返します。
//
// エミュレータをプロセス内で動かすため、docker も認証情報も要りません。これが無いと
// 読み書き・一覧・存在確認の実装（このパッケージの大半）が 1 行も実行されないまま
// になります。疑似ディレクトリの扱いのように、壊れても静かなロジックがここにあります。
func newTestStore(t *testing.T, objects ...fakestorage.Object) remoteio.Store {
	t.Helper()

	server, err := fakestorage.NewServerWithOptions(fakestorage.Options{
		InitialObjects:  objects,
		BucketsLocation: "US",
	})
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	server.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: testBucket})

	return remoteio.NewStore(gcs.NewHandler(server.Client()))
}

func object(name, content string) fakestorage.Object {
	return fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{BucketName: testBucket, Name: name},
		Content:     []byte(content),
	}
}

func uri(name string) string { return remoteio.BuildURI(gcs.Scheme, testBucket, name) }

func collect(t *testing.T, seq iter.Seq2[remoteio.Entry, error]) []remoteio.Entry {
	t.Helper()
	var out []remoteio.Entry
	for entry, err := range seq {
		require.NoError(t, err)
		out = append(out, entry)
	}
	return out
}

func names(entries []remoteio.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

func TestOpenAndStat(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, object("data/report.txt", "hello gcs"))

	t.Run("既存オブジェクトを読める", func(t *testing.T) {
		data, err := remoteio.ReadAll(ctx, store, uri("data/report.txt"))
		require.NoError(t, err)
		assert.Equal(t, "hello gcs", string(data))
	})

	// スキームに依らず errors.Is(err, remoteio.ErrNotExist) で判定できることが、
	// このライブラリの抽象が成立するための条件です。
	t.Run("不在は ErrNotExist を包んで返す", func(t *testing.T) {
		_, err := store.Open(ctx, uri("data/missing.txt"))
		assert.ErrorIs(t, err, remoteio.ErrNotExist)

		_, err = store.Stat(ctx, uri("data/missing.txt"))
		assert.ErrorIs(t, err, remoteio.ErrNotExist)
	})

	t.Run("Stat がサイズと URI を返す", func(t *testing.T) {
		info, err := store.Stat(ctx, uri("data/report.txt"))
		require.NoError(t, err)
		assert.Equal(t, uri("data/report.txt"), info.URI)
		assert.Equal(t, int64(len("hello gcs")), info.Size)
	})

	t.Run("Exists は不在を (false, nil) で返す", func(t *testing.T) {
		ok, err := store.Exists(ctx, uri("data/missing.txt"))
		require.NoError(t, err)
		assert.False(t, ok)

		ok, err = store.Exists(ctx, uri("data/report.txt"))
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("オブジェクト名が空の URI を拒否する", func(t *testing.T) {
		_, err := store.Open(ctx, "gs://"+testBucket)
		assert.ErrorIs(t, err, remoteio.ErrInvalidURI)
	})
}

func TestList(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t,
		object("data/a.txt", "a"),
		object("data/sub/b.txt", "b"),
		object("data-archive/c.txt", "c"),
	)

	t.Run("区切り文字ありは直下のみ、疑似ディレクトリは IsPrefix", func(t *testing.T) {
		entries := collect(t, store.List(ctx, uri("data"), remoteio.WithDelimiter("/")))
		assert.ElementsMatch(t, []string{"a.txt", "sub/"}, names(entries))

		for _, e := range entries {
			if e.Name == "sub/" {
				assert.True(t, e.IsPrefix, "attrs.Prefix を型のついた情報として渡すこと")
				assert.Equal(t, uri("data/sub/"), e.URI)
			} else {
				assert.False(t, e.IsPrefix)
			}
		}
	})

	t.Run("区切り文字なしは配下を再帰的に返す", func(t *testing.T) {
		entries := collect(t, store.List(ctx, uri("data")))
		assert.ElementsMatch(t, []string{"a.txt", "sub/b.txt"}, names(entries))
	})

	// 素の前方一致にすると、"data" が "data-archive/" のオブジェクトまで拾います。
	t.Run("プレフィックスは常に正規化され隣接する名前を拾わない", func(t *testing.T) {
		entries := collect(t, store.List(ctx, uri("data")))
		for _, e := range entries {
			assert.NotContains(t, e.URI, "data-archive")
		}
	})

	t.Run("break で打ち切れる", func(t *testing.T) {
		var seen int
		for _, err := range store.List(ctx, uri("data")) {
			require.NoError(t, err)
			seen++
			break
		}
		assert.Equal(t, 1, seen)
	})
}

func TestWriteAndDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	t.Run("書いた内容を読み戻せる", func(t *testing.T) {
		require.NoError(t, remoteio.WriteAll(ctx, store, uri("out/a.txt"), []byte("written"),
			remoteio.WithContentType("text/plain"),
			remoteio.WithCacheControl("no-store"),
		))

		info, err := store.Stat(ctx, uri("out/a.txt"))
		require.NoError(t, err)
		assert.Equal(t, "text/plain", info.ContentType)

		data, err := remoteio.ReadAll(ctx, store, uri("out/a.txt"))
		require.NoError(t, err)
		assert.Equal(t, "written", string(data))
	})

	t.Run("メタデータを保存する", func(t *testing.T) {
		require.NoError(t, remoteio.WriteAll(ctx, store, uri("out/meta.txt"), []byte("x"),
			remoteio.WithMetadata(map[string]string{"job-id": "j1"}),
		))

		info, err := store.Stat(ctx, uri("out/meta.txt"))
		require.NoError(t, err)
		assert.Equal(t, "j1", info.Metadata["job-id"])
	})

	t.Run("削除は冪等", func(t *testing.T) {
		require.NoError(t, store.Delete(ctx, uri("out/a.txt")))
		assert.NoError(t, store.Delete(ctx, uri("out/a.txt")))

		ok, err := store.Exists(ctx, uri("out/a.txt"))
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

// failingReader は途中まで読めたあとに失敗するリーダーです。
type failingReader struct{ r io.Reader }

func (f *failingReader) Read(p []byte) (int, error) {
	if n, _ := f.r.Read(p); n > 0 {
		return n, nil
	}
	return 0, errors.New("読み取り失敗")
}

// 書き込みの原子性の回帰テストです。
//
// 失敗経路で storage.Writer.Close() を呼ぶと、Close はアップロードを「完了」させる
// API なので、失敗した書き込みが切り詰められたオブジェクトとして残ります。
// 既存オブジェクトの上書き中なら元のデータが壊れます。実際にそうなっていた経路で、
// このテストはそこを固定しています。
func TestWriteIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, object("out/existing.txt", "original"))

	t.Run("失敗した書き込みはオブジェクトを作らない", func(t *testing.T) {
		err := store.Write(ctx, uri("out/partial.txt"), &failingReader{r: strings.NewReader("partial-data")})
		require.Error(t, err)

		ok, existsErr := store.Exists(ctx, uri("out/partial.txt"))
		require.NoError(t, existsErr)
		assert.False(t, ok, "失敗した書き込みでオブジェクトが残ってはいけない")
	})

	t.Run("失敗した上書きは既存の内容を壊さない", func(t *testing.T) {
		err := store.Write(ctx, uri("out/existing.txt"), &failingReader{r: strings.NewReader("partial-data")})
		require.Error(t, err)

		data, readErr := remoteio.ReadAll(ctx, store, uri("out/existing.txt"))
		require.NoError(t, readErr)
		assert.Equal(t, "original", string(data))
	})
}

func TestWriteIfNotExists(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	require.NoError(t, remoteio.WriteAll(ctx, store, uri("once.txt"), []byte("first"), remoteio.WithIfNotExists()))

	err := remoteio.WriteAll(ctx, store, uri("once.txt"), []byte("second"), remoteio.WithIfNotExists())
	assert.ErrorIs(t, err, remoteio.ErrExist)

	data, err := remoteio.ReadAll(ctx, store, uri("once.txt"))
	require.NoError(t, err)
	assert.Equal(t, "first", string(data), "既存の内容が保たれること")
}

// サーバーサイドコピーの確認です。
//
// これが無いと、利用側は CopierFrom を自前で呼ぶために別の GCS クライアントを
// 持つことになります。
// Copier を実装していることをコンパイル時に固定します。
// これが外れると Copy は黙ってストリーム中継へ落ち、遅くなるだけで誰も気づきません。
var _ remoteio.Copier = (*gcs.Handler)(nil)

func TestCopyUsesServerSideCopy(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, object("src/video.mp4", "payload"))

	t.Run("同一スキームのコピー", func(t *testing.T) {
		require.NoError(t, store.Copy(ctx, uri("src/video.mp4"), uri("dst/video.mp4")))

		data, err := remoteio.ReadAll(ctx, store, uri("dst/video.mp4"))
		require.NoError(t, err)
		assert.Equal(t, "payload", string(data))

		ok, err := store.Exists(ctx, uri("src/video.mp4"))
		require.NoError(t, err)
		assert.True(t, ok, "Copy はコピー元を残す")
	})

	t.Run("コピー元が無ければ ErrNotExist", func(t *testing.T) {
		err := store.Copy(ctx, uri("src/missing.mp4"), uri("dst/missing.mp4"))
		assert.ErrorIs(t, err, remoteio.ErrNotExist)
	})
}

// gs:// のストアからローカルパスも読めることの確認です。
// 開発時にローカルファイルへ差し替えられることが、この組み合わせの狙いです。
func TestStoreAlsoHandlesLocalPaths(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	dir := t.TempDir()

	require.NoError(t, remoteio.WriteAll(ctx, store, dir+"/local.txt", []byte("local")))
	data, err := remoteio.ReadAll(ctx, store, dir+"/local.txt")
	require.NoError(t, err)
	assert.Equal(t, "local", string(data))

	t.Run("担当外のリモートスキームは明確に拒否する", func(t *testing.T) {
		_, err := store.Open(ctx, "s3://other/key")
		assert.ErrorIs(t, err, remoteio.ErrUnsupportedScheme)
	})
}

// ハンドラは公開されているため直接呼ばれる余地があります。
// 担当外のスキームを黙って処理すると、gs:// のつもりで s3:// のバケットを読みます。
func TestHandlerRejectsForeignScheme(t *testing.T) {
	ctx := context.Background()
	server, err := fakestorage.NewServerWithOptions(fakestorage.Options{BucketsLocation: "US"})
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	h := gcs.NewHandler(server.Client())
	_, err = h.Open(ctx, "s3://other-bucket/key")
	assert.ErrorIs(t, err, remoteio.ErrInvalidURI)
}
