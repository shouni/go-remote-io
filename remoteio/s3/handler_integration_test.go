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

// newTestStore returns a Store connected to an in-process S3 fake.
//
// Neither docker nor AWS credentials are needed. The fake listens on plain HTTP, which
// is the same condition as an S3-compatible endpoint without TLS (self-hosted MinIO or
// R2). Under this condition, calling PutObject directly cannot write a non-Seeker body at all.
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
		// The fake cannot resolve virtual-hosted-style bucket names, so it talks path-style.
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

	t.Run("reads an existing object", func(t *testing.T) {
		data, err := remoteio.ReadAll(ctx, store, uri("data/report.txt"))
		require.NoError(t, err)
		assert.Equal(t, "hello s3", string(data))
	})

	t.Run("missing object wraps ErrNotExist", func(t *testing.T) {
		_, err := store.Open(ctx, uri("data/missing.txt"))
		assert.ErrorIs(t, err, remoteio.ErrNotExist)

		// HeadObject returns NotFound rather than NoSuchKey, so both are checked.
		_, err = store.Stat(ctx, uri("data/missing.txt"))
		assert.ErrorIs(t, err, remoteio.ErrNotExist)
	})

	t.Run("Exists returns (false, nil) when missing", func(t *testing.T) {
		ok, err := store.Exists(ctx, uri("data/missing.txt"))
		require.NoError(t, err)
		assert.False(t, ok)

		ok, err = store.Exists(ctx, uri("data/report.txt"))
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("rejects a URI with an empty object name", func(t *testing.T) {
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

	t.Run("with delimiter lists direct children only, pseudo-directories are IsPrefix", func(t *testing.T) {
		entries := collect(t, store.List(ctx, uri("data"), remoteio.WithDelimiter("/")))
		assert.ElementsMatch(t, []string{"a.txt", "sub/"}, names(entries))

		for _, e := range entries {
			if e.Name == "sub/" {
				assert.True(t, e.IsPrefix, "CommonPrefixes are passed on as typed information")
				assert.Equal(t, uri("data/sub/"), e.URI)
			} else {
				assert.False(t, e.IsPrefix)
			}
		}
	})

	t.Run("without delimiter lists recursively", func(t *testing.T) {
		entries := collect(t, store.List(ctx, uri("data")))
		assert.ElementsMatch(t, []string{"a.txt", "sub/b.txt"}, names(entries))
	})

	t.Run("prefix is always normalized and skips adjacent names", func(t *testing.T) {
		entries := collect(t, store.List(ctx, uri("data")))
		for _, e := range entries {
			assert.NotContains(t, e.URI, "data-archive")
		}
	})
}

func TestWriteAndDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, nil)

	t.Run("reads back what was written", func(t *testing.T) {
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

	t.Run("delete is idempotent", func(t *testing.T) {
		require.NoError(t, store.Delete(ctx, uri("out/a.txt")))
		assert.NoError(t, store.Delete(ctx, uri("out/a.txt")))
	})
}

// nonSeeker is a plain io.Reader that does not implement Seeker.
// GCS's storage.Reader and HTTP response bodies take this shape.
type nonSeeker struct{ r io.Reader }

func (n *nonSeeker) Read(p []byte) (int, error) { return n.r.Read(p) }

// A regression test for writing a non-Seeker body.
//
// Passing a raw io.Reader to PutObject always fails against a non-TLS endpoint with
// "unseekable stream is not supported without TLS and trailing checksum". Production
// AWS (https) passes, so it is easy to miss; this is the path that broke for plain
// MinIO and R2 endpoints, and for cross-cloud Copy.
func TestWriteAcceptsNonSeekableBody(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, nil)

	err := store.Write(ctx, uri("out/stream.txt"), &nonSeeker{r: strings.NewReader("streamed body")})
	require.NoError(t, err, "transfermanager can handle non-Seeker streams")

	data, err := remoteio.ReadAll(ctx, store, uri("out/stream.txt"))
	require.NoError(t, err)
	assert.Equal(t, "streamed body", string(data))
}

// failingReader is a reader that fails after part of it has been read.
type failingReader struct{ r io.Reader }

func (f *failingReader) Read(p []byte) (int, error) {
	if n, _ := f.r.Read(p); n > 0 {
		return n, nil
	}
	return 0, errors.New("read failed")
}

func TestWriteIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, map[string]string{"out/existing.txt": "original"})

	t.Run("failed write creates no object", func(t *testing.T) {
		err := store.Write(ctx, uri("out/partial.txt"), &failingReader{r: strings.NewReader("partial-data")})
		require.Error(t, err)

		ok, existsErr := store.Exists(ctx, uri("out/partial.txt"))
		require.NoError(t, existsErr)
		assert.False(t, ok)
	})

	t.Run("failed overwrite keeps existing content", func(t *testing.T) {
		err := store.Write(ctx, uri("out/existing.txt"), &failingReader{r: strings.NewReader("partial-data")})
		require.Error(t, err)

		data, readErr := remoteio.ReadAll(ctx, store, uri("out/existing.txt"))
		require.NoError(t, readErr)
		assert.Equal(t, "original", string(data))
	})
}

// Checks conditional writes.
//
// Leaving the decision to the storage-side If-None-Match avoids the race of checking
// with Exists and then calling Write (another process slipping in between the check and the write).
func TestWriteIfNotExists(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, nil)

	require.NoError(t, remoteio.WriteAll(ctx, store, uri("once.txt"), []byte("first"), remoteio.WithIfNotExists()))

	err := remoteio.WriteAll(ctx, store, uri("once.txt"), []byte("second"), remoteio.WithIfNotExists())
	if err == nil {
		// This is reached when gofakes3 does not interpret conditional requests.
		// It is a difference from the real S3, so it is recorded rather than passed silently.
		t.Skip("the fake does not interpret If-None-Match, so this path can be verified only against the real S3")
	}
	assert.ErrorIs(t, err, remoteio.ErrExist)

	data, err := remoteio.ReadAll(ctx, store, uri("once.txt"))
	require.NoError(t, err)
	assert.Equal(t, "first", string(data), "the existing contents are kept")
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
	assert.True(t, ok, "Copy keeps the source")
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

	// Through Store it is filtered out at dispatch, but handlers are exported and can be
	// called directly.
	h := s3.NewHandler(awss3.New(awss3.Options{Region: s3.DefaultRegion}))
	_, err := h.Open(ctx, "gs://other-bucket/key")
	assert.ErrorIs(t, err, remoteio.ErrInvalidURI)

	_ = store
}

func TestSignURL(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, map[string]string{"a.txt": "x"})

	t.Run("supports GET and PUT", func(t *testing.T) {
		for _, method := range []string{"GET", "PUT"} {
			signed, err := store.SignURL(ctx, uri("a.txt"), method, 5*60)
			require.NoError(t, err)
			assert.Contains(t, signed, "X-Amz-Signature")
		}
	})

	t.Run("unsupported method returns ErrNotSupported", func(t *testing.T) {
		_, err := store.SignURL(ctx, uri("a.txt"), "DELETE", 60)
		assert.ErrorIs(t, err, remoteio.ErrNotSupported)
	})
}
