package s3_test

import (
	"context"
	"errors"
	"io"
	"iter"
	"net/http/httptest"
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

// newTestStore は、インプロセスの S3 フェイクに接続した Store を返します。
//
// docker も AWS 認証情報も要りません。フェイクは平文 HTTP で待ち受けるため、
// TLS が無い S3 互換エンドポイント（MinIO や R2 のセルフホスト）と同じ条件になります。
// v1 の PutObject 直呼びはこの条件で非 Seeker のボディを一切書けませんでした。
func newTestStore(t *testing.T, objects map[string]string) remoteio.Store {
	t.Helper()

	backend := s3mem.New()
	faker := gofakes3.New(backend)
	server := httptest.NewServer(faker.Server())
	t.Cleanup(server.Close)

	require.NoError(t, backend.CreateBucket(testBucket))

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(s3.DefaultRegion),
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

	return remoteio.NewStore(s3.NewHandler(client))
}

func uri(name string) string { return remoteio.BuildURI(s3.Scheme, testBucket, name) }

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
	store := newTestStore(t, map[string]string{"data/report.txt": "hello s3"})

	t.Run("既存オブジェクトを読める", func(t *testing.T) {
		data, err := remoteio.ReadAll(ctx, store, uri("data/report.txt"))
		require.NoError(t, err)
		assert.Equal(t, "hello s3", string(data))
	})

	t.Run("不在は ErrNotExist を包んで返す", func(t *testing.T) {
		_, err := store.Open(ctx, uri("data/missing.txt"))
		assert.ErrorIs(t, err, remoteio.ErrNotExist)

		// HeadObject は NoSuchKey ではなく NotFound を返すため、両方を見ています。
		_, err = store.Stat(ctx, uri("data/missing.txt"))
		assert.ErrorIs(t, err, remoteio.ErrNotExist)
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
		_, err := store.Open(ctx, "s3://"+testBucket)
		assert.ErrorIs(t, err, remoteio.ErrInvalidURI)
	})
}

func TestList(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, map[string]string{
		"data/a.txt":         "a",
		"data/sub/b.txt":     "b",
		"data-archive/c.txt": "c",
	})

	t.Run("区切り文字ありは直下のみ、疑似ディレクトリは IsPrefix", func(t *testing.T) {
		entries := collect(t, store.List(ctx, uri("data"), remoteio.WithDelimiter("/")))
		assert.ElementsMatch(t, []string{"a.txt", "sub/"}, names(entries))

		for _, e := range entries {
			if e.Name == "sub/" {
				assert.True(t, e.IsPrefix, "CommonPrefixes を型のついた情報として渡すこと")
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

	t.Run("プレフィックスは常に正規化され隣接する名前を拾わない", func(t *testing.T) {
		entries := collect(t, store.List(ctx, uri("data")))
		for _, e := range entries {
			assert.NotContains(t, e.URI, "data-archive")
		}
	})
}

func TestWriteAndDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, nil)

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

	t.Run("削除は冪等", func(t *testing.T) {
		require.NoError(t, store.Delete(ctx, uri("out/a.txt")))
		assert.NoError(t, store.Delete(ctx, uri("out/a.txt")))
	})
}

// nonSeeker は Seeker を実装しない純粋な io.Reader です。
// GCS の storage.Reader や HTTP レスポンスボディがこの形になります。
type nonSeeker struct{ r io.Reader }

func (n *nonSeeker) Read(p []byte) (int, error) { return n.r.Read(p) }

// 非 Seeker のボディを書けることの回帰テストです。
//
// v1 は PutObject へ生の io.Reader を渡していたため、TLS でないエンドポイントでは
// 「unseekable stream is not supported without TLS and trailing checksum」で
// 必ず失敗しました。本番の AWS (https) は通るので気づきにくく、MinIO や R2 の
// 平文エンドポイント、そしてクロスクラウドの Copy が壊れていた経路です。
func TestWriteAcceptsNonSeekableBody(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, nil)

	err := store.Write(ctx, uri("out/stream.txt"), &nonSeeker{r: strings.NewReader("streamed body")})
	require.NoError(t, err, "transfermanager は非 Seeker のストリームを扱えること")

	data, err := remoteio.ReadAll(ctx, store, uri("out/stream.txt"))
	require.NoError(t, err)
	assert.Equal(t, "streamed body", string(data))
}

// failingReader は途中まで読めたあとに失敗するリーダーです。
type failingReader struct{ r io.Reader }

func (f *failingReader) Read(p []byte) (int, error) {
	if n, _ := f.r.Read(p); n > 0 {
		return n, nil
	}
	return 0, errors.New("読み取り失敗")
}

func TestWriteIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, map[string]string{"out/existing.txt": "original"})

	t.Run("失敗した書き込みはオブジェクトを作らない", func(t *testing.T) {
		err := store.Write(ctx, uri("out/partial.txt"), &failingReader{r: strings.NewReader("partial-data")})
		require.Error(t, err)

		ok, existsErr := store.Exists(ctx, uri("out/partial.txt"))
		require.NoError(t, existsErr)
		assert.False(t, ok)
	})

	t.Run("失敗した上書きは既存の内容を壊さない", func(t *testing.T) {
		err := store.Write(ctx, uri("out/existing.txt"), &failingReader{r: strings.NewReader("partial-data")})
		require.Error(t, err)

		data, readErr := remoteio.ReadAll(ctx, store, uri("out/existing.txt"))
		require.NoError(t, readErr)
		assert.Equal(t, "original", string(data))
	})
}

// 条件付き書き込みの確認です。
//
// 判定をストレージ側の If-None-Match に委ねることで、Exists で確かめてから
// Write するときの競合（確認と書き込みの間に他のプロセスが割り込む）を避けます。
func TestWriteIfNotExists(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, nil)

	require.NoError(t, remoteio.WriteAll(ctx, store, uri("once.txt"), []byte("first"), remoteio.WithIfNotExists()))

	err := remoteio.WriteAll(ctx, store, uri("once.txt"), []byte("second"), remoteio.WithIfNotExists())
	if err == nil {
		// gofakes3 が条件付きリクエストを解釈しない場合はここへ来ます。
		// 本物の S3 との差分なので、黙って通さずに記録します。
		t.Skip("フェイクが If-None-Match を解釈しないため、この経路は本物の S3 でのみ検証できます")
	}
	assert.ErrorIs(t, err, remoteio.ErrExist)

	data, err := remoteio.ReadAll(ctx, store, uri("once.txt"))
	require.NoError(t, err)
	assert.Equal(t, "first", string(data), "既存の内容が保たれること")
}

func TestCopyUsesServerSideCopy(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, map[string]string{"src/video.mp4": "payload"})

	var _ remoteio.Copier = (*s3.Handler)(nil)

	require.NoError(t, store.Copy(ctx, uri("src/video.mp4"), uri("dst/video.mp4")))

	data, err := remoteio.ReadAll(ctx, store, uri("dst/video.mp4"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(data))

	ok, err := store.Exists(ctx, uri("src/video.mp4"))
	require.NoError(t, err)
	assert.True(t, ok, "Copy はコピー元を残す")
}

func TestStoreAlsoHandlesLocalPaths(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, nil)
	dir := t.TempDir()

	require.NoError(t, remoteio.WriteAll(ctx, store, dir+"/local.txt", []byte("local")))
	data, err := remoteio.ReadAll(ctx, store, dir+"/local.txt")
	require.NoError(t, err)
	assert.Equal(t, "local", string(data))

	_, err = store.Open(ctx, "gs://other/key")
	assert.ErrorIs(t, err, remoteio.ErrUnsupportedScheme)
}

func TestHandlerRejectsForeignScheme(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, nil)

	// Store 経由では振り分けで弾かれますが、ハンドラは公開されているため
	// 直接呼ばれる余地があります。
	h := s3.NewHandler(awss3.New(awss3.Options{Region: s3.DefaultRegion}))
	_, err := h.Open(ctx, "gs://other-bucket/key")
	assert.ErrorIs(t, err, remoteio.ErrInvalidURI)

	_ = store
}

func TestSignURL(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, map[string]string{"a.txt": "x"})

	t.Run("GET と PUT に対応する", func(t *testing.T) {
		for _, method := range []string{"GET", "PUT"} {
			signed, err := store.SignURL(ctx, uri("a.txt"), method, 5*60)
			require.NoError(t, err)
			assert.Contains(t, signed, "X-Amz-Signature")
		}
	})

	t.Run("対応しないメソッドは ErrNotSupported", func(t *testing.T) {
		_, err := store.SignURL(ctx, uri("a.txt"), "DELETE", 60)
		assert.ErrorIs(t, err, remoteio.ErrNotSupported)
	})
}
