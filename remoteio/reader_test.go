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

// 1. ローカルリソース（読み込み、一覧、存在確認）のテスト
func TestUniversalInputReader_Local(t *testing.T) {
	ctx := context.Background()
	reader := NewUniversalInputReader(nil, nil)

	// テスト用の一時ディレクトリを作成
	tmpDir, err := os.MkdirTemp("", "remoteio_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	content := "Hello, InputReader!"
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(tmpFile, []byte(content), 0644)
	require.NoError(t, err)

	// --- Reader (Open) のテスト ---
	t.Run("Open: success reading local file", func(t *testing.T) {
		rc, err := reader.Open(ctx, tmpFile)
		require.NoError(t, err)
		defer rc.Close()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, content, string(got))
	})

	// --- Lister (List) のテスト ---
	t.Run("List: handles various local directory scenarios", func(t *testing.T) {
		anotherFile := filepath.Join(tmpDir, "another.log")
		require.NoError(t, os.WriteFile(anotherFile, []byte("log"), 0644))
		nestedDir := filepath.Join(tmpDir, "nested")
		require.NoError(t, os.Mkdir(nestedDir, 0755))
		nestedFile := filepath.Join(nestedDir, "nested.txt")
		require.NoError(t, os.WriteFile(nestedFile, []byte("nested"), 0644))

		var files []string
		err := reader.List(ctx, tmpDir, func(path string) error {
			files = append(files, path)
			return nil
		})
		require.NoError(t, err)

		expected := []string{tmpFile, anotherFile}
		assert.ElementsMatch(t, expected, files)
	})

	t.Run("List: stops and returns callback error", func(t *testing.T) {
		expectedErr := errors.New("stop listing")

		err := reader.List(ctx, tmpDir, func(_ string) error {
			return expectedErr
		})

		assert.ErrorIs(t, err, expectedErr)
	})

	// --- Exister (Exists) のテスト ---
	t.Run("Exists: local file scenarios", func(t *testing.T) {
		// 存在するファイル
		exists, err := reader.Exists(ctx, tmpFile)
		assert.NoError(t, err)
		assert.True(t, exists)

		// 存在するディレクトリ
		exists, err = reader.Exists(ctx, tmpDir)
		assert.NoError(t, err)
		assert.True(t, exists)

		// 存在しないパス
		exists, err = reader.Exists(ctx, filepath.Join(tmpDir, "not_exist.txt"))
		assert.NoError(t, err)
		assert.False(t, exists)
	})
}

// 2. URI 振り分けとバリデーションのテスト (Open, List, Exists)
func TestUniversalInputReader_DispatchAndValidation(t *testing.T) {
	ctx := context.Background()
	reader := NewUniversalInputReader(nil, nil)

	tests := []struct {
		name        string
		path        string
		op          string // "Open", "List", "Exists"
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
			name:        "Exists GCS - no client",
			path:        "gs://my-bucket/obj",
			op:          "Exists",
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
		{
			name:        "Exists S3 - no client",
			path:        "s3://my-bucket/obj",
			op:          "Exists",
			expectedErr: "S3クライアントが未初期化です",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			var exists bool
			switch tt.op {
			case "Open":
				_, err = reader.Open(ctx, tt.path)
			case "List":
				err = reader.List(ctx, tt.path, func(string) error { return nil })
			case "Exists":
				exists, err = reader.Exists(ctx, tt.path)
				assert.False(t, exists)
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

// 3. インターフェース満足度のテスト
func TestInputReader_InterfaceSatisfaction(_ *testing.T) {
	var _ Reader = (*UniversalInputReader)(nil)
	var _ Lister = (*UniversalInputReader)(nil)
	var _ Exister = (*UniversalInputReader)(nil)
	var _ InputReader = (*UniversalInputReader)(nil)
}
