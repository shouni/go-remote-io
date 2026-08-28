package s3_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/s3"
	"github.com/shouni/go-remote-io/remoteio/storetest"
)

// 本物の S3 ハンドラを、memio やローカルと同じ 1 本のスイートに通します。
func TestConformance(t *testing.T) {
	storetest.TestHandler(t, func(t *testing.T) storetest.Fixture {
		backend := s3mem.New()
		server := httptest.NewServer(gofakes3.New(backend).Server())
		t.Cleanup(server.Close)
		require.NoError(t, backend.CreateBucket(testBucket))

		cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
			awsconfig.WithRegion(s3.DefaultRegion),
			awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		)
		require.NoError(t, err)

		client := awss3.NewFromConfig(cfg, func(o *awss3.Options) {
			o.BaseEndpoint = aws.String(server.URL)
			o.UsePathStyle = true
		})

		return storetest.Fixture{
			Handler:             s3.NewHandler(client),
			Root:                remoteio.BuildURI(s3.Scheme, testBucket, "conformance"),
			SupportsContentType: true,
			SupportsMetadata:    true,
			SupportsIfNotExists: true,
			BucketScoped:        true,
		}
	})
}
