package gcs_test

import (
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
	"github.com/shouni/go-remote-io/remoteio/storetest"
)

// 本物の GCS ハンドラを、memio やローカルと同じ 1 本のスイートに通します。
// 実装ごとに別のテストを書いていると、契約の食い違いはテストの外に残ります。
func TestConformance(t *testing.T) {
	storetest.TestHandler(t, func(t *testing.T) storetest.Fixture {
		server, err := fakestorage.NewServerWithOptions(fakestorage.Options{BucketsLocation: "US"})
		require.NoError(t, err)
		t.Cleanup(server.Stop)
		server.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: testBucket})

		return storetest.Fixture{
			Handler:             gcs.NewHandler(server.Client()),
			Root:                remoteio.BuildURI(gcs.Scheme, testBucket, "conformance"),
			SupportsContentType: true,
			SupportsMetadata:    true,
			SupportsIfNotExists: true,
			BucketScoped:        true,
		}
	})
}
