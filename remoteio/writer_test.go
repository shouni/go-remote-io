package remoteio

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 1. ローカル書き込み・削除のテスト (実ファイル操作)
func TestUniversalIOWriter_Local(t *testing.T) {
	ctx := context.Background()
	writer := NewUniversalIOWriter(nil, nil)

	tmpDir, err := os.MkdirTemp("", "remoteio_writer_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	targetPath := filepath.Join(tmpDir, "sub/dir/test.txt")

	t.Run("success writing to local file and directory creation", func(t *testing.T) {
		content := "Hello, Local IO!"
		reader := bytes.NewReader([]byte(content))

		err := writer.Write(ctx, targetPath, reader, "text/plain")
		require.NoError(t, err)

		got, err := os.ReadFile(targetPath)
		require.NoError(t, err)
		assert.Equal(t, content, string(got))
	})

	t.Run("success deleting local file", func(t *testing.T) {
		// 事前に存在を確認
		_, err := os.Stat(targetPath)
		require.NoError(t, err)

		// 削除実行
		err = writer.Delete(ctx, targetPath)
		assert.NoError(t, err)

		// 消えていることを確認
		_, err = os.Stat(targetPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("delete is idempotent (no error if file not exists)", func(t *testing.T) {
		nonExistentPath := filepath.Join(tmpDir, "already_gone.txt")
		err := writer.Delete(ctx, nonExistentPath)
		assert.NoError(t, err, "削除対象がなくてもエラーを返すべきではありません")
	})
}

// 2. クラウドURIの振り分け（ディスパッチ）ロジックのテスト
func TestUniversalIOWriter_Dispatch(t *testing.T) {
	ctx := context.Background()
	writer := NewUniversalIOWriter(nil, nil)
	content := bytes.NewReader([]byte("test content"))

	tests := []struct {
		name        string
		uri         string
		op          string // "Write" or "Delete"
		expectedErr string
	}{
		{
			name:        "Write GCS - client error",
			uri:         "gs://my-bucket/obj",
			op:          "Write",
			expectedErr: "GCSクライアントが初期化されていません",
		},
		{
			name:        "Write S3 - client error",
			uri:         "s3://my-bucket/obj",
			op:          "Write",
			expectedErr: "S3クライアントが初期化されていません",
		},
		{
			name:        "Delete GCS - client error",
			uri:         "gs://my-bucket/obj",
			op:          "Delete",
			expectedErr: "GCSクライアントが未初期化です",
		},
		{
			name:        "Delete S3 - client error",
			uri:         "s3://my-bucket/obj",
			op:          "Delete",
			expectedErr: "S3クライアントが未初期化です",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.op == "Write" {
				err = writer.Write(ctx, tt.uri, content, "text/plain")
			} else {
				err = writer.Delete(ctx, tt.uri)
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

// 3. インターフェース満足度のテスト
func TestOutputWriter_InterfaceSatisfaction(t *testing.T) {
	var _ Writer = (*UniversalIOWriter)(nil)
	var _ Remover = (*UniversalIOWriter)(nil)
	var _ OutputWriter = (*UniversalIOWriter)(nil)
}
