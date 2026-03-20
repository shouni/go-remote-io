package remoteio

import (
	"context"
	"errors"
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

	// --- List のテスト (エッジケースを含む) ---
	t.Run("List: handles various local directory scenarios", func(t *testing.T) {
		// 準備：サブディレクトリと追加ファイルを作成
		subDir := filepath.Join(tmpDir, "subdir")
		require.NoError(t, os.Mkdir(subDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(subDir, "subfile.txt"), []byte("sub"), 0644))

		anotherFile := filepath.Join(tmpDir, "another.log")
		require.NoError(t, os.WriteFile(anotherFile, []byte("log"), 0644))

		// 実行：コールバックでファイルパスを収集
		var files []string
		err := reader.List(ctx, tmpDir, func(path string) error {
			files = append(files, path)
			return nil
		})
		require.NoError(t, err)

		// 検証：サブディレクトリは含まれず、直下のファイルのみが返されることを確認
		expected := []string{tmpFile, anotherFile}
		assert.ElementsMatch(t, expected, files)
	})

	t.Run("List: success listing empty directory", func(t *testing.T) {
		emptyDir, err := os.MkdirTemp("", "empty_dir")
		require.NoError(t, err)
		defer os.RemoveAll(emptyDir)

		var files []string
		err = reader.List(ctx, emptyDir, func(path string) error {
			files = append(files, path)
			return nil
		})
		require.NoError(t, err)
		assert.Empty(t, files, "空のディレクトリをリストした場合、結果は空であるべきです")
	})

	t.Run("List: propagates callback error", func(t *testing.T) {
		expectedErr := errors.New("callback failed")
		err := reader.List(ctx, tmpDir, func(path string) error {
			return expectedErr
		})
		assert.ErrorIs(t, err, expectedErr)
	})

	// ✨ 新規追加：ディレクトリではなくファイルパスを渡した場合のテスト
	t.Run("List: error when path is a file", func(t *testing.T) {
		var files []string
		err := reader.List(ctx, tmpFile, func(path string) error {
			files = append(files, path)
			return nil
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ローカルディレクトリの読み込みに失敗しました")
		assert.Empty(t, files, "ファイルパスを指定した場合、コールバックは一度も呼ばれないべきです")
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
				err = reader.List(ctx, tt.path, func(string) error { return nil })
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
