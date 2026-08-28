package remoteio_test

import (
	"path/filepath"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/storetest"
)

// ローカルファイルシステムは Content-Type もメタデータも保持しません。
// 対応していないことを宣言した上で、それ以外の契約は同じように満たします。
func TestLocalHandlerConformance(t *testing.T) {
	storetest.TestHandler(t, func(t *testing.T) storetest.Fixture {
		return storetest.Fixture{
			Handler:             remoteio.NewLocalHandler(),
			Root:                t.TempDir(),
			SupportsIfNotExists: true,
		}
	})
}

// file:// はローカルへ読み替えるだけですが、URI の組み立てと読み替えが
// 往復で崩れていないかは別途確かめる価値があります。
func TestFileHandlerConformance(t *testing.T) {
	storetest.TestHandler(t, func(t *testing.T) storetest.Fixture {
		return storetest.Fixture{
			Handler:             remoteio.NewFileHandler(),
			Root:                "file://" + filepath.ToSlash(t.TempDir()),
			SupportsIfNotExists: true,
		}
	})
}
