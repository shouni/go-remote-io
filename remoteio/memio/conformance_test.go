package memio_test

import (
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/memio"
	"github.com/shouni/go-remote-io/remoteio/storetest"
)

// memio が本物と同じ契約を満たすことが、このパッケージの存在意義そのものです。
// フェイクと本物がずれるなら、フェイクを使うテストは何も確かめていません。
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
