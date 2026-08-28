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

// このファイルは外部テストパッケージ (remoteio_test) です。
// remoteio 本体の依存にクラウド SDK を入れないための境界なので、
// gcs / s3 を import するテストはここか各スキームのパッケージに置きます。

const bucket = "test-bucket"

// newMultiStore は、GCS と S3 の両方のフェイクを 1 つの Store に束ねて返します。
//
// v1 はこれを MultiFactory と HandlerProvider という専用の足場で実現していました。
// ファクトリが Handler を返せば NewStore に並べるだけで済むため、両方とも要りません。
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
		assert.Equal(t, []string{"file", "gs", "s3"}, router.Schemes(), "辞書順で安定していること")
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

	// クロスクラウドのコピーは、コピー元のストリームが io.Seeker ではありません。
	// v1 の S3 側は PutObject 直呼びだったため、平文エンドポイントではここが
	// 「unseekable stream is not supported without TLS」で必ず失敗していました。
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
		// v1 は署名器が独立インターフェースだったため、束ねる側が signerRouter という
		// 専用の振り分けを別に持っていました。Signer を任意インターフェースにしたので
		// 振り分けは Router の 1 箇所だけです。
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

// Sub でスコープを絞る形が、両クラウドで同じように使えることの確認です。
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

			// ルートのストアから完全な URI でも同じものが読めます。
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
