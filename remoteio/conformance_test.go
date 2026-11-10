package remoteio_test

import (
	"path/filepath"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/storetest"
)

// The local file system keeps neither Content-Type nor metadata.
// Having declared that, it meets the rest of the contract in the same way.
func TestLocalHandlerConformance(t *testing.T) {
	storetest.TestHandler(t, func(t *testing.T) storetest.Fixture {
		return storetest.Fixture{
			Handler:             remoteio.NewLocalHandler(),
			Root:                t.TempDir(),
			SupportsIfNotExists: true,
		}
	})
}

// file:// only maps to local paths, but it is still worth checking separately that
// building and mapping the URI survives the round trip.
func TestFileHandlerConformance(t *testing.T) {
	storetest.TestHandler(t, func(t *testing.T) storetest.Fixture {
		return storetest.Fixture{
			Handler:             remoteio.NewFileHandler(),
			Root:                "file://" + filepath.ToSlash(t.TempDir()),
			SupportsIfNotExists: true,
		}
	})
}
