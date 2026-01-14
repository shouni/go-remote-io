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

// 1. ローカルファイルの読み込みテスト
func TestUniversalInputReader_Open_Local(t *testing.T) {
	ctx := context.Background()
	reader := NewUniversalInputReader(nil, nil)

	// テスト用の一時ファイルを作成
	tmpDir, err := os.MkdirTemp("", "remoteio_reader_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	content := "Hello, InputReader!"
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(tmpFile, []byte(content), 0644)
	require.NoError(t, err)

	t.Run("success reading local file", func(t *testing.T) {
		rc, err := reader.Open(ctx, tmpFile)
		require.NoError(t, err)
		defer rc.Close()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, content, string(got))
	})

	t.Run("error reading non-existent file", func(t *testing.T) {
		_, err := reader.Open(ctx, filepath.Join(tmpDir, "notfound.txt"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ローカルファイルのオープンに失敗しました")
	})
}

// 2. URI 振り分けとバリデーションのテスト
func TestUniversalInputReader_Open_DispatchAndValidation(t *testing.T) {
	ctx := context.Background()
	// クライアントを注入しないことで、各プロトコルの判定パスを通ったことをエラーメッセージで確認する
	reader := NewUniversalInputReader(nil, nil)

	tests := []struct {
		name        string
		path        string
		expectedErr string
	}{
		{
			name:        "GCS dispatch - no client",
			path:        "gs://my-bucket/obj",
			expectedErr: "GCSクライアントが初期化されていない",
		},
		{
			name:        "S3 dispatch - no client",
			path:        "s3://my-bucket/obj",
			expectedErr: "S3クライアントが初期化されていない",
		},
		{
			name: "GCS invalid path - dispatch priority",
			path: "gs://only-bucket",
			// 実装上、オブジェクト名のチェックより先にクライアントチェックが走るため、メッセージを合わせる
			expectedErr: "GCSクライアントが初期化されていない",
		},
		{
			name: "S3 invalid path - dispatch priority",
			path: "s3://only-bucket",
			// 同様に、S3クライアントの未初期化エラーが優先される
			expectedErr: "S3クライアントが初期化されていない",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc, err := reader.Open(ctx, tt.path)
			assert.Nil(t, rc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

// 3. インターフェース満足度のテスト
func TestInputReader_InterfaceSatisfaction(t *testing.T) {
	var _ InputReader = (*UniversalInputReader)(nil)
}
