package remoteio

import "mime"

// DefaultContentType は、Content-Type が指定されなかった場合に使用されるデフォルト値です。
const DefaultContentType = "text/plain; charset=utf-8"

// WriteSettings は WriteOption を解決した結果です。
//
// 公開しているのは ListSettings と同じ理由です。SchemeHandler を実装する側が
// オプションを解釈できなければ、Content-Type や Cache-Control を黙って無視する
// 実装しか書けなくなります。
type WriteSettings struct {
	// ContentType は書き込むオブジェクトの MIME タイプです。空なら DefaultContentType。
	ContentType string
	// CacheControl は Cache-Control ヘッダーです。空なら設定しません。
	CacheControl string
	// ContentDisposition は Content-Disposition ヘッダーです。空なら設定しません。
	ContentDisposition string
	// Metadata は任意のユーザー定義メタデータです。空なら設定しません。
	// ローカルファイルシステムでは保存されず、無視されます。
	Metadata map[string]string
}

// WriteOption は Functional Options パターンのための関数型
type WriteOption func(*WriteSettings)

// NewWriteSettings はオプションを畳み込んで設定を組み立てます。
// Content-Type が空の場合は DefaultContentType で補います。
func NewWriteSettings(opts ...WriteOption) WriteSettings {
	settings := WriteSettings{ContentType: DefaultContentType}
	for _, opt := range opts {
		if opt != nil {
			opt(&settings)
		}
	}
	// 明示的に空文字が渡された場合のフォールバックを一元管理
	if settings.ContentType == "" {
		settings.ContentType = DefaultContentType
	}
	return settings
}

// WithContentType は MIME タイプを指定する
func WithContentType(contentType string) WriteOption {
	return func(s *WriteSettings) {
		s.ContentType = contentType
	}
}

// WithMetadata はユーザー定義メタデータを指定する。
// 複数回渡した場合は積み上がります（同じキーは後勝ち）。
// ローカル書き込みでは Content-Type などと同じく黙って無視されます。
func WithMetadata(metadata map[string]string) WriteOption {
	return func(s *WriteSettings) {
		if len(metadata) == 0 {
			return
		}
		if s.Metadata == nil {
			s.Metadata = make(map[string]string, len(metadata))
		}
		for k, v := range metadata {
			s.Metadata[k] = v
		}
	}
}

// WithCacheControl は Cache-Control ヘッダーを指定する
func WithCacheControl(cacheControl string) WriteOption {
	return func(s *WriteSettings) {
		s.CacheControl = cacheControl
	}
}

// WithInline はブラウザでのインライン再生を指示する
func WithInline() WriteOption {
	return func(s *WriteSettings) {
		s.ContentDisposition = "inline"
	}
}

// WithAttachment はダウンロードを強制し、ファイル名を指定する
func WithAttachment(filename string) WriteOption {
	return func(s *WriteSettings) {
		if filename != "" {
			s.ContentDisposition = mime.FormatMediaType("attachment", map[string]string{"filename": filename})
		} else {
			s.ContentDisposition = "attachment"
		}
	}
}
