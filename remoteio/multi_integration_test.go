package remoteio_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
	"github.com/shouni/go-remote-io/remoteio/s3"
)

// This file is an external test package (remoteio_test).
// It is the boundary that keeps cloud SDKs out of the dependencies of remoteio itself,
// so tests importing gcs / s3 go here or in each scheme's package.

const bucket = "test-bucket"

// newMultiStore returns one Store bundling both the GCS and S3 fakes.
func newMultiStore(t *testing.T) remoteio.Store {
	t.Helper()

	gcsServer, err := fakestorage.NewServerWithOptions(fakestorage.Options{BucketsLocation: "US"})
	require.NoError(t, err)
	t.Cleanup(gcsServer.Stop)
	gcsServer.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: bucket})

	gcsFactory, err := gcs.New(context.Background(), gcs.WithClient(gcsServer.Client()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = gcsFactory.Close() })

	backend := s3mem.New()
	s3Server := httptest.NewServer(gofakes3.New(backend).Server())
	t.Cleanup(s3Server.Close)
	require.NoError(t, backend.CreateBucket(bucket))

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(s3.DefaultRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)

	s3Factory, err := s3.New(context.Background(), s3.WithClient(
		awss3.NewFromConfig(cfg, func(o *awss3.Options) {
			o.BaseEndpoint = aws.String(s3Server.URL)
			o.UsePathStyle = true
		}),
	))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s3Factory.Close() })

	gcsHandler, err := gcsFactory.Handler()
	require.NoError(t, err)
	s3Handler, err := s3Factory.Handler()
	require.NoError(t, err)

	return remoteio.NewStore(gcsHandler, s3Handler)
}

func gcsURI(name string) string { return remoteio.BuildURI(gcs.Scheme, bucket, name) }
func s3URI(name string) string  { return remoteio.BuildURI(s3.Scheme, bucket, name) }

func TestMultiSchemeStore(t *testing.T) {
	ctx := context.Background()
	store := newMultiStore(t)

	t.Run("両スキームが登録される", func(t *testing.T) {
		router, ok := store.(*remoteio.Router)
		require.True(t, ok)
		assert.Equal(t, []string{"file", "gs", "s3"}, router.Schemes(), "stable in lexical order")
	})

	t.Run("同じストアで両方へ読み書きできる", func(t *testing.T) {
		require.NoError(t, remoteio.WriteAll(ctx, store, gcsURI("a.txt"), []byte("from gcs")))
		require.NoError(t, remoteio.WriteAll(ctx, store, s3URI("a.txt"), []byte("from s3")))

		got, err := remoteio.ReadAll(ctx, store, gcsURI("a.txt"))
		require.NoError(t, err)
		assert.Equal(t, "from gcs", string(got))

		got, err = remoteio.ReadAll(ctx, store, s3URI("a.txt"))
		require.NoError(t, err)
		assert.Equal(t, "from s3", string(got))
	})

	// In a cross-cloud copy, the source stream is not an io.Seeker.
	// If the S3 side called PutObject directly, this would always fail against a plain
	// endpoint with "unseekable stream is not supported without TLS".
	t.Run("GCS から S3 へコピーできる", func(t *testing.T) {
		require.NoError(t, remoteio.WriteAll(ctx, store, gcsURI("src/payload.bin"), []byte("cross-cloud")))
		require.NoError(t, store.Copy(ctx, gcsURI("src/payload.bin"), s3URI("dst/payload.bin")))

		got, err := remoteio.ReadAll(ctx, store, s3URI("dst/payload.bin"))
		require.NoError(t, err)
		assert.Equal(t, "cross-cloud", string(got))
	})

	t.Run("S3 から GCS へコピーできる", func(t *testing.T) {
		require.NoError(t, remoteio.WriteAll(ctx, store, s3URI("src/back.bin"), []byte("back")))
		require.NoError(t, store.Copy(ctx, s3URI("src/back.bin"), gcsURI("dst/back.bin")))

		got, err := remoteio.ReadAll(ctx, store, gcsURI("dst/back.bin"))
		require.NoError(t, err)
		assert.Equal(t, "back", string(got))
	})

	t.Run("署名器はスキームごとに振り分けられる", func(t *testing.T) {
		signed, err := store.SignURL(ctx, s3URI("a.txt"), "GET", 60)
		require.NoError(t, err)
		assert.Contains(t, signed, "X-Amz-Signature")
	})

	t.Run("ローカルパスも同じストアで扱える", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, remoteio.WriteAll(ctx, store, dir+"/local.txt", []byte("local")))

		got, err := remoteio.ReadAll(ctx, store, dir+"/local.txt")
		require.NoError(t, err)
		assert.Equal(t, "local", string(got))
	})

	t.Run("未登録スキームは ErrUnsupportedScheme", func(t *testing.T) {
		_, err := store.Open(ctx, "azure://container/blob")
		assert.ErrorIs(t, err, remoteio.ErrUnsupportedScheme)
	})
}

// Checks that narrowing the scope with Sub works the same way for both clouds.
func TestSubAcrossSchemes(t *testing.T) {
	ctx := context.Background()
	store := newMultiStore(t)

	for name, base := range map[string]string{"gcs": gcsURI("jobs"), "s3": s3URI("jobs")} {
		t.Run(name, func(t *testing.T) {
			jobs := store.Sub(base)

			require.NoError(t, remoteio.WriteAll(ctx, jobs, "j1/status.json", []byte(`{"state":"done"}`)))

			data, err := remoteio.ReadAll(ctx, jobs, "j1/status.json")
			require.NoError(t, err)
			assert.JSONEq(t, `{"state":"done"}`, string(data))

			// The same object can be read from the root store with the full URI too.
			data, err = remoteio.ReadAll(ctx, store, base+"/j1/status.json")
			require.NoError(t, err)
			assert.JSONEq(t, `{"state":"done"}`, string(data))

			var ids []string
			for entry, err := range jobs.List(ctx, "", remoteio.WithDelimiter("/")) {
				require.NoError(t, err)
				if entry.IsPrefix {
					ids = append(ids, entry.Name)
				}
			}
			assert.Equal(t, []string{"j1/"}, ids)
		})
	}
}
