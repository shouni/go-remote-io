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

// failingReader は、途中まで読めてから失敗する io.Reader です。
type failingReader struct {
	sent bool
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, r.err
	}
	r.sent = true
	return copy(p, []byte("partial")), nil
}

// 書き込みに失敗しても、中途半端なファイルを残さないこと。
//
// 以前は os.Create に直接書いていたため、ctx のキャンセルや I/O エラーで抜けると
// 途中まで書かれたファイルが残りました。リモート側は失敗すればオブジェクトが
// できないので、ここだけ挙動が違うと呼び出し側が両方に備えることになります。
func TestLocalWriteIsAtomic(t *testing.T) {
	ctx := context.Background()
	writer := NewSchemeRouter()

	t.Run("失敗しても既存の内容を壊さない", func(t *testing.T) {
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "existing.txt")
		require.NoError(t, os.WriteFile(target, []byte("original"), 0o644))

		err := writer.Write(ctx, target, &failingReader{err: assert.AnError})
		require.ErrorIs(t, err, assert.AnError)

		got, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "original", string(got), "失敗した書き込みが既存の内容を壊しています")
	})

	t.Run("失敗しても新しいファイルを作らない", func(t *testing.T) {
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "new.txt")

		err := writer.Write(ctx, target, &failingReader{err: assert.AnError})
		require.ErrorIs(t, err, assert.AnError)

		assert.NoFileExists(t, target)
	})

	t.Run("ctx キャンセルでも一時ファイルを残さない", func(t *testing.T) {
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "canceled.txt")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := writer.Write(ctx, target, bytes.NewReader([]byte("data")))
		require.Error(t, err)

		entries, err := os.ReadDir(tmpDir)
		require.NoError(t, err)
		assert.Empty(t, entries, "一時ファイルが残っています")
	})

	t.Run("成功したファイルは 0644 で置かれる", func(t *testing.T) {
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "mode.txt")

		require.NoError(t, writer.Write(ctx, target, bytes.NewReader([]byte("data"))))

		info, err := os.Stat(target)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	})
}

// WithMetadata は積み上がり、同じキーは後勝ちになること。
// SchemeHandler の実装側が New*Settings で同じ解決結果を得られる必要があるため、
// オプションの畳み込み方をここで固定します。
func TestWithMetadata(t *testing.T) {
	t.Run("複数回渡すと積み上がる", func(t *testing.T) {
		settings := NewWriteSettings(
			WithMetadata(map[string]string{"a": "1"}),
			WithMetadata(map[string]string{"b": "2"}),
		)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, settings.Metadata)
	})

	t.Run("同じキーは後勝ち", func(t *testing.T) {
		settings := NewWriteSettings(
			WithMetadata(map[string]string{"a": "1"}),
			WithMetadata(map[string]string{"a": "2"}),
		)
		assert.Equal(t, map[string]string{"a": "2"}, settings.Metadata)
	})

	t.Run("空を渡しても nil のまま", func(t *testing.T) {
		assert.Nil(t, NewWriteSettings(WithMetadata(nil)).Metadata)
	})

	// 渡した map を後から書き換えても設定に影響しないこと。
	t.Run("渡した map は取り込まれる", func(t *testing.T) {
		source := map[string]string{"a": "1"}
		settings := NewWriteSettings(WithMetadata(source))
		source["a"] = "changed"
		assert.Equal(t, "1", settings.Metadata["a"])
	})
}
