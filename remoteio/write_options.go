package remoteio

import "mime"

// writeConfig は内部的な設定値を保持する構造体
type writeConfig struct {
	contentType        string
	cacheControl       string
	contentDisposition string
}

// WriteOption は Functional Options パターンのための関数型
type WriteOption func(*writeConfig)

// WithContentType は MIME タイプを指定する
func WithContentType(contentType string) WriteOption {
	return func(c *writeConfig) {
		c.contentType = contentType
	}
}

// WithCacheControl は Cache-Control ヘッダーを指定する
func WithCacheControl(cacheControl string) WriteOption {
	return func(c *writeConfig) {
		c.cacheControl = cacheControl
	}
}

// WithInline はブラウザでのインライン再生を指示する
func WithInline() WriteOption {
	return func(c *writeConfig) {
		c.contentDisposition = "inline"
	}
}

// WithAttachment はダウンロードを強制し、ファイル名を指定する
func WithAttachment(filename string) WriteOption {
	return func(c *writeConfig) {
		if filename != "" {
			c.contentDisposition = mime.FormatMediaType("attachment", map[string]string{"filename": filename})
		} else {
			c.contentDisposition = "attachment"
		}
	}
}
