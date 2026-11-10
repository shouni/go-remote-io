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

// newTestStore returns a Store connected to an in-process GCS fake.
//
// The emulator runs in-process, so neither docker nor credentials are needed. Without
// it, the read, write, listing and existence-check implementations (most of this
// package) would never run a single line. Logic that stays quiet when broken, such as
// pseudo-directory handling, lives here.
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

	// That errors.Is(err, remoteio.ErrNotExist) detects it whatever the scheme is the
	// condition for this library's abstraction to work.
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
				assert.True(t, e.IsPrefix, "attrs.Prefix is passed on as typed information")
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

	// With a bare prefix match, "data" also picks up objects in "data-archive/".
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

// failingReader is a reader that fails after part of it has been read.
type failingReader struct{ r io.Reader }

func (f *failingReader) Read(p []byte) (int, error) {
	if n, _ := f.r.Read(p); n > 0 {
		return n, nil
	}
	return 0, errors.New("read failed")
}

// A regression test for write atomicity.
//
// Calling storage.Writer.Close() on the failure path — Close being the API that
// "completes" the upload — leaves the failed write behind as a truncated object.
// While overwriting an existing object, the original data is broken. That path really
// did this, and this test pins it.
func TestWriteIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, object("out/existing.txt", "original"))

	t.Run("失敗した書き込みはオブジェクトを作らない", func(t *testing.T) {
		err := store.Write(ctx, uri("out/partial.txt"), &failingReader{r: strings.NewReader("partial-data")})
		require.Error(t, err)

		ok, existsErr := store.Exists(ctx, uri("out/partial.txt"))
		require.NoError(t, existsErr)
		assert.False(t, ok, "a failed write must not leave an object behind")
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
	assert.Equal(t, "first", string(data), "the existing contents are kept")
}

// Checks server-side copy.
//
// Without it, consumers hold a separate GCS client just to call CopierFrom themselves.
// That Copier is implemented is pinned at compile time.
// If it came off, Copy would silently fall back to the stream relay and just get
// slower, without anyone noticing.
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
		assert.True(t, ok, "Copy keeps the source")
	})

	t.Run("コピー元が無ければ ErrNotExist", func(t *testing.T) {
		err := store.Copy(ctx, uri("src/missing.mp4"), uri("dst/missing.mp4"))
		assert.ErrorIs(t, err, remoteio.ErrNotExist)
	})
}

// Checks that a gs:// store can also read local paths.
// The aim of this pairing is being able to swap in local files during development.
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

// Handlers are exported, so they can be called directly.
// Silently processing a foreign scheme would read an s3:// bucket while meaning gs://.
func TestHandlerRejectsForeignScheme(t *testing.T) {
	ctx := context.Background()
	server, err := fakestorage.NewServerWithOptions(fakestorage.Options{BucketsLocation: "US"})
	require.NoError(t, err)
	t.Cleanup(server.Stop)

	h := gcs.NewHandler(server.Client())
	_, err = h.Open(ctx, "s3://other-bucket/key")
	assert.ErrorIs(t, err, remoteio.ErrInvalidURI)
}
