package remoteio

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 1. ローカルファイルの読み込みとリスティングのテスト
func TestUniversalInputReader_Local(t *testing.T) {
	ctx := context.Background()
	reader := NewUniversalInputReader(nil, nil)

	// テスト用の一時ディレクトリとファイルを作成
	tmpDir, err := os.MkdirTemp("", "remoteio_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	content := "Hello, InputReader!"
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(tmpFile, []byte(content), 0644)
	require.NoError(t, err)

	// --- Open のテスト ---
	t.Run("Open: success reading local file", func(t *testing.T) {
		rc, err := reader.Open(ctx, tmpFile)
		require.NoError(t, err)
		defer rc.Close()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, content, string(got))
	})

	t.Run("Open: error reading non-existent file", func(t *testing.T) {
		_, err := reader.Open(ctx, filepath.Join(tmpDir, "notfound.txt"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ローカルファイルのオープンに失敗しました")
	})

	// --- List のテスト (新規追加なのだ！) ---
	t.Run("List: success listing local directory", func(t *testing.T) {
		files, err := reader.List(ctx, tmpDir)
		require.NoError(t, err)
		assert.Len(t, files, 1)
		assert.Equal(t, tmpFile, files[0])
	})
}

// 2. URI 振り分けとバリデーションのテスト (Open & List)
func TestUniversalInputReader_DispatchAndValidation(t *testing.T) {
	ctx := context.Background()
	reader := NewUniversalInputReader(nil, nil)

	tests := []struct {
		name        string
		path        string
		op          string // "Open" or "List"
		expectedErr string
	}{
		{
			name:        "Open GCS - no client",
			path:        "gs://my-bucket/obj",
			op:          "Open",
			expectedErr: "GCSクライアントが未初期化です",
		},
		{
			name:        "List GCS - no client",
			path:        "gs://my-bucket/prefix",
			op:          "List",
			expectedErr: "GCSクライアントが未初期化です",
		},
		{
			name:        "Open S3 - no client",
			path:        "s3://my-bucket/obj",
			op:          "Open",
			expectedErr: "S3クライアントが未初期化です",
		},
		{
			name:        "List S3 - no client",
			path:        "s3://my-bucket/prefix",
			op:          "List",
			expectedErr: "S3クライアントが未初期化です",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.op == "Open" {
				_, err = reader.Open(ctx, tt.path)
			} else {
				_, err = reader.List(ctx, tt.path)
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

// 3. インターフェース満足度のテスト
// Listメソッドが実装されていないとここでコンパイルエラーになるのだ！
func TestInputReader_InterfaceSatisfaction(t *testing.T) {
	var _ InputReader = (*UniversalInputReader)(nil)
}
