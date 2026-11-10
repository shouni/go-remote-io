package gcs_test

import (
	"testing"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/gcs"
	"github.com/shouni/go-remote-io/remoteio/storetest"
)

// Runs the real GCS handler through the same single suite as memio and local.
// With a separate test per implementation, contract mismatches stay outside the tests.
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
