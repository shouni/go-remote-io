package remoteio

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// file:// は他のツールや設定ファイルから渡ってくる形です。以前は未対応スキームとして
// 弾かれていたため、ローカルを指しているのに読めないという状態でした。
func TestFileHandler(t *testing.T) {
	ctx := context.Background()
	router := NewSchemeRouter()

	tmpDir := t.TempDir()
	fileURI := func(elem ...string) string {
		return PrefixFile + filepath.ToSlash(filepath.Join(append([]string{tmpDir}, elem...)...))
	}

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello"), 0o644))

	t.Run("Open: file:// URI で読める", func(t *testing.T) {
		rc, err := router.Open(ctx, fileURI("a.txt"))
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(got))
	})

	t.Run("Open: 不在は os.ErrNotExist を包む", func(t *testing.T) {
		_, err := router.Open(ctx, fileURI("missing.txt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("Exists", func(t *testing.T) {
		exists, err := router.Exists(ctx, fileURI("a.txt"))
		require.NoError(t, err)
		assert.True(t, exists)

		exists, err = router.Exists(ctx, fileURI("missing.txt"))
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("Write と Delete", func(t *testing.T) {
		target := fileURI("sub", "written.txt")
		require.NoError(t, router.Write(ctx, target, strings.NewReader("written")))

		got, err := os.ReadFile(filepath.Join(tmpDir, "sub", "written.txt"))
		require.NoError(t, err)
		assert.Equal(t, "written", string(got))

		require.NoError(t, router.Delete(ctx, target))
		exists, err := router.Exists(ctx, target)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// GCS / S3 のハンドラが gs:// / s3:// の URI を返すのに合わせ、
	// 列挙結果もそのまま次の呼び出しへ渡せる形（file:// 付き）で返します。
	t.Run("List: 結果は file:// URI で返る", func(t *testing.T) {
		var got []string
		require.NoError(t, router.List(ctx, PrefixFile+filepath.ToSlash(tmpDir), func(p string) error {
			got = append(got, p)
			return nil
		}))
		assert.Contains(t, got, fileURI("a.txt"))
		for _, p := range got {
			assert.True(t, strings.HasPrefix(p, PrefixFile), "file:// で始まるべき: %s", p)
		}
	})

	// 名前に空白を含むファイルは、URI としては %20 で渡ってきます。
	t.Run("パーセントエンコードをデコードする", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "my file.txt"), []byte("space"), 0o644))

		rc, err := router.Open(ctx, PrefixFile+filepath.ToSlash(tmpDir)+"/my%20file.txt")
		require.NoError(t, err)
		defer func() { _ = rc.Close() }()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, "space", string(got))
	})

	t.Run("パスが空の URI は拒否する", func(t *testing.T) {
		_, err := router.Open(ctx, PrefixFile)
		assert.ErrorContains(t, err, "パスが指定されていません")
	})
}

// NewSchemeRouter が組み立てる 3 つの担当（リモート / ローカル / file://）の確認。
func TestNewSchemeRouterRegistersLocalSchemes(t *testing.T) {
	router := NewSchemeRouter()
	assert.ElementsMatch(t, []string{PrefixFile}, router.Schemes())

	ctx := context.Background()
	_, err := router.Open(ctx, "gs://bucket/obj")
	assert.ErrorContains(t, err, "未対応のURIスキームです")
}
