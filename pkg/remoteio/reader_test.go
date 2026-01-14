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

	// テスト用の一時ディレクトリを作成
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

	// --- List のテスト (エッジケース拡充版なのだ！) ---
	t.Run("List: handles various local directory scenarios", func(t *testing.T) {
		// 準備：サブディレクトリと追加ファイルを作成
		subDir := filepath.Join(tmpDir, "subdir")
		require.NoError(t, os.Mkdir(subDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(subDir, "subfile.txt"), []byte("sub"), 0644))

		anotherFile := filepath.Join(tmpDir, "another.log")
		require.NoError(t, os.WriteFile(anotherFile, []byte("log"), 0644))

		// 実行
		files, err := reader.List(ctx, tmpDir)
		require.NoError(t, err)

		// 検証：サブディレクトリは含まれず、直下のファイルのみが返されることを確認
		// ファイルの並び順に依存しないよう ElementsMatch を使うのだ
		expected := []string{tmpFile, anotherFile}
		assert.ElementsMatch(t, expected, files)
	})

	t.Run("List: success listing empty directory", func(t *testing.T) {
		emptyDir, err := os.MkdirTemp("", "empty_dir")
		require.NoError(t, err)
		defer os.RemoveAll(emptyDir)

		files, err := reader.List(ctx, emptyDir)
		require.NoError(t, err)
		assert.Empty(t, files, "空のディレクトリは空のスライスを返すべきなのだ")
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
func TestInputReader_InterfaceSatisfaction(t *testing.T) {
	var _ InputReader = (*UniversalInputReader)(nil)
}
