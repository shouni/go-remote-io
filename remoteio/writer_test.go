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

	tmpDir, err := os.MkdirTemp("", "remoteio_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	t.Run("success writing to local file", func(t *testing.T) {
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
			name:        "GCS path dispatch",
			uri:         "gs://my-bucket/path/to/obj",
			expectedErr: "GCSクライアントが初期化されていません",
		},
		{
			name:        "S3 path dispatch",
			uri:         "s3://my-bucket/path/to/obj",
			expectedErr: "S3クライアントが初期化されていません",
		},
		{
			name: "Invalid GCS URI",
			uri:  "gs://",
			// ParseGCSURI がエラーを返すようになったため、Write メソッドが返すエラープレフィックスを期待値にします
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

// 3. パラメータバリデーションのテスト
func TestUniversalIOWriter_Validation(t *testing.T) {
	ctx := context.Background()
	writer := NewUniversalIOWriter(nil, nil)

	t.Run("empty bucket name for GCS", func(t *testing.T) {
		err := writer.WriteToGCS(ctx, "", "path", nil, "")
		assert.ErrorContains(t, err, "バケット名が空です")
	})

	t.Run("empty object path for S3", func(t *testing.T) {
		err := writer.WriteToS3(ctx, "bucket", "", nil, "")
		assert.ErrorContains(t, err, "バケット名またはオブジェクトパスが空です")
	})
}

// 4. インターフェース満足度のテスト
func TestInterfaceSatisfaction(t *testing.T) {
	var _ OutputWriter = (*UniversalIOWriter)(nil)
}
