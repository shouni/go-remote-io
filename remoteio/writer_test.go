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
func TestRouterLocalWrite(t *testing.T) {
	ctx := context.Background()
	writer := NewRouter(NewLocalHandler())

	tmpDir, err := os.MkdirTemp("", "remoteio_writer_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	targetPath := filepath.Join(tmpDir, "sub/dir/test.txt")

	t.Run("success writing to local file with all options", func(t *testing.T) {
		content := "Hello, Local IO with Cache!"
		reader := bytes.NewReader([]byte(content))

		// CacheControl を含めたすべてのオプションを渡して、パニックやエラーが起きないことを確認
		err := writer.Write(ctx, targetPath, reader,
			WithContentType("text/plain"),
			WithInline(),
			WithCacheControl("public, max-age=31536000, immutable"),
		)
		require.NoError(t, err, "オプションを指定してもローカル書き込みは成功すべきです")

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

	t.Run("write is cancelled when context is already done", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		cancelledPath := filepath.Join(tmpDir, "cancelled.txt")
		reader := bytes.NewReader([]byte("should not be written"))

		err := writer.Write(cancelledCtx, cancelledPath, reader)
		require.ErrorIs(t, err, context.Canceled)

		_, statErr := os.Stat(cancelledPath)
		assert.True(t, os.IsNotExist(statErr), "キャンセル済みcontextではファイルを作成すべきではありません")
	})
}

// 2. 登録されていないスキームは、書き込みも削除も明確に拒否されること。
//
// 以前は「クライアントが未初期化です」というエラーで、対応していないのか
// 設定を忘れたのかが呼び出し側から区別できませんでした。
func TestRouterWriteRejectsUnregisteredScheme(t *testing.T) {
	ctx := context.Background()
	writer := NewRouter(NewLocalHandler())
	content := bytes.NewReader([]byte("test content"))

	tests := []struct {
		name        string
		uri         string
		op          string // "Write" or "Delete"
		withCache   bool   // CacheControl オプションを付与するか
		expectedErr string
	}{
		{
			name:        "Write GCS - 未登録スキーム",
			uri:         "gs://my-bucket/obj",
			op:          "Write",
			withCache:   true,
			expectedErr: "未対応のURIスキームです",
		},
		{
			name:        "Write S3 - 未登録スキーム",
			uri:         "s3://my-bucket/obj",
			op:          "Write",
			withCache:   true,
			expectedErr: "未対応のURIスキームです",
		},
		{
			name:        "Delete GCS - 未登録スキーム",
			uri:         "gs://my-bucket/obj",
			op:          "Delete",
			expectedErr: "未対応のURIスキームです",
		},
		{
			name:        "Delete S3 - 未登録スキーム",
			uri:         "s3://my-bucket/obj",
			op:          "Delete",
			expectedErr: "未対応のURIスキームです",
		},
		{
			name:        "Write Local - Invalid path",
			uri:         "/", // ルートディレクトリへの書き込み試行
			op:          "Write",
			expectedErr: "is a directory", // OS依存だがエラーになることを確認
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.op == "Write" {
				opts := []WriteOption{WithContentType("text/plain")}
				if tt.withCache {
					opts = append(opts, WithCacheControl("public, max-age=3600"))
				}
				err = writer.Write(ctx, tt.uri, content, opts...)
			} else {
				err = writer.Delete(ctx, tt.uri)
			}

			require.Error(t, err)
			// ローカルパスで "/" を指定した際などは環境によってエラー内容が異なる可能性があるため
			// クラウドURIの場合のみ詳細なメッセージを確認
			if IsGCSURI(tt.uri) || IsS3URI(tt.uri) {
				assert.Contains(t, err.Error(), tt.expectedErr)
			}
		})
	}
}

// 3. インターフェース満足度のテスト
func TestOutputWriter_InterfaceSatisfaction(_ *testing.T) {
	// 定義したインターフェースを Router が満たしているかコンパイル時にチェック
	var _ Writer = (*Router)(nil)
	var _ Remover = (*Router)(nil)
	var _ OutputWriter = (*Router)(nil)
}
