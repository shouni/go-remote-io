package memio_test

import (
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/memio"
	"github.com/shouni/go-remote-io/remoteio/storetest"
)

// That memio meets the same contract as the real thing is the very reason this package exists.
// If the fake drifts from the real thing, tests using the fake verify nothing.
func TestConformance(t *testing.T) {
	storetest.TestHandler(t, func(_ *testing.T) storetest.Fixture {
		return storetest.Fixture{
			Handler:             memio.New(),
			Root:                remoteio.BuildURI(memio.DefaultScheme, "bucket", "conformance"),
			SupportsContentType: true,
			SupportsMetadata:    true,
			SupportsIfNotExists: true,
			BucketScoped:        true,
		}
	})
}
