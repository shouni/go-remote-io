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

// 1. ローカル書き込みのテスト (実ファイル操作)
func TestUniversalIOWriter_WriteToLocal(t *testing.T) {
	ctx := context.Background()
	writer := NewUniversalIOWriter(nil, nil)

	tmpDir, err := os.MkdirTemp("", "remoteio_writer_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	t.Run("success writing to local file and directory creation", func(t *testing.T) {
		// ネストしたディレクトリへの書き込みテスト（自動生成されるか確認）
		targetPath := filepath.Join(tmpDir, "sub/dir/test.txt")
		content := "Hello, Local IO!"
		reader := bytes.NewReader([]byte(content))

		err := writer.Write(ctx, targetPath, reader, "text/plain")
		require.NoError(t, err)

		got, err := os.ReadFile(targetPath)
		require.NoError(t, err)
		assert.Equal(t, content, string(got))
	})
}

// 2. クラウドURIの振り分け（ディスパッチ）ロジックのテスト
func TestUniversalIOWriter_Write_Dispatch(t *testing.T) {
	ctx := context.Background()
	writer := NewUniversalIOWriter(nil, nil)
	content := bytes.NewReader([]byte("test content"))

	tests := []struct {
		name        string
		uri         string
		expectedErr string
	}{
		{
			name:        "GCS path dispatch - client error",
			uri:         "gs://my-bucket/path/to/obj",
			expectedErr: "GCSクライアントが初期化されていません",
		},
		{
			name:        "S3 path dispatch - client error",
			uri:         "s3://my-bucket/path/to/obj",
			expectedErr: "S3クライアントが初期化されていません",
		},
		{
			name:        "Invalid GCS URI format",
			uri:         "gs://",
			expectedErr: "GCS URIのパースに失敗しました",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := writer.Write(ctx, tt.uri, content, "text/plain")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

// 3. パラメータバリデーションのテスト (内部メソッドの直接テスト)
func TestUniversalIOWriter_InternalValidation(t *testing.T) {
	ctx := context.Background()
	writer := NewUniversalIOWriter(nil, nil)

	t.Run("empty bucket name for GCS", func(t *testing.T) {
		err := writer.WriteToGCS(ctx, "", "path", nil, "")
		assert.ErrorContains(t, err, "バケット名が空です")
	})

	t.Run("empty object path for S3", func(t *testing.T) {
		err := writer.WriteToS3(ctx, "bucket", "", nil, "")
		assert.ErrorContains(t, err, "S3への書き込みに失敗しました: オブジェクトパスが空です")
	})
}

// 4. インターフェース満足度のテスト
func TestOutputWriter_InterfaceSatisfaction(t *testing.T) {
	// 具象構造体が分割したすべての書き込みインターフェースを満たしているか確認
	var _ Writer = (*UniversalIOWriter)(nil)
	var _ OutputWriter = (*UniversalIOWriter)(nil)
}
