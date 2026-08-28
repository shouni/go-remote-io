package remoteio

import (
	"maps"
	"mime"
)

// DefaultContentType は、Content-Type が指定されなかった場合に使用される既定値です。
const DefaultContentType = "text/plain; charset=utf-8"

// ListOptions は ListOption を解決した結果で、Handler.List が受け取ります。
//
// 解決関数を公開していないのは、実装対象が Handler 1 本に絞られているためです。
// 実装側は解決済みの値だけを受け取ります。
type ListOptions struct {
	// Delimiter は階層の区切り文字です。
	// 空文字ならプレフィックス配下を再帰的に列挙します。
	// 指定すると直下のみが対象になり、畳まれた階層が IsPrefix の Entry として現れます。
	Delimiter string
}

// ListOption は List の挙動を変える Functional Option です。
type ListOption func(*ListOptions)

// WithDelimiter は階層の区切り文字を指定し、プレフィックス直下のみを対象にします。
//
// 指定すると、直下のオブジェクトに加えて「疑似ディレクトリ」が列挙されます。
// 疑似ディレクトリは IsPrefix が真の Entry として渡るため、呼び出し側は
// 末尾の文字を見て判定する必要がありません。
//
// これが要るのはジョブ ID の一覧のような場面です。区切り文字なしでは配下の
// 全オブジェクトが返るため、1 ジョブに 3 つ成果物があれば 3 倍のデータを受け取って
// 呼び出し側で重複を潰すことになります。区切り文字はその走査をサーバー側へ寄せます。
func WithDelimiter(delimiter string) ListOption {
	return func(o *ListOptions) { o.Delimiter = delimiter }
}

// newListOptions はオプションを畳み込んで設定を組み立てます。
func newListOptions(opts ...ListOption) ListOptions {
	var o ListOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// WriteOptions は WriteOption を解決した結果で、Handler.Write が受け取ります。
type WriteOptions struct {
	// ContentType は書き込むオブジェクトの MIME タイプです。空なら DefaultContentType。
	ContentType string
	// CacheControl は Cache-Control ヘッダーです。空なら設定しません。
	CacheControl string
	// ContentDisposition は Content-Disposition ヘッダーです。空なら設定しません。
	ContentDisposition string
	// Metadata は任意のユーザー定義メタデータです。空なら設定しません。
	// ローカルファイルシステムでは保存されず、無視されます。
	Metadata map[string]string
	// IfNotExists は、対象が存在しない場合にのみ書き込むことを指示します。
	// 既に存在した場合、Write は ErrExist を含むエラーを返します。
	IfNotExists bool
}

// WriteOption は Write の挙動を変える Functional Option です。
type WriteOption func(*WriteOptions)

// newWriteOptions はオプションを畳み込んで設定を組み立てます。
// Content-Type が空の場合は DefaultContentType で補います。
func newWriteOptions(opts ...WriteOption) WriteOptions {
	o := WriteOptions{ContentType: DefaultContentType}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	// 明示的に空文字が渡された場合のフォールバックを一元管理します。
	if o.ContentType == "" {
		o.ContentType = DefaultContentType
	}
	return o
}

// WithContentType は MIME タイプを指定します。
func WithContentType(contentType string) WriteOption {
	return func(o *WriteOptions) { o.ContentType = contentType }
}

// WithCacheControl は Cache-Control ヘッダーを指定します。
func WithCacheControl(cacheControl string) WriteOption {
	return func(o *WriteOptions) { o.CacheControl = cacheControl }
}

// WithMetadata はユーザー定義メタデータを指定します。
// 複数回渡した場合は積み上がります（同じキーは後勝ち）。
// ローカル書き込みでは Content-Type などと同じく黙って無視されます。
func WithMetadata(metadata map[string]string) WriteOption {
	return func(o *WriteOptions) {
		if len(metadata) == 0 {
			return
		}
		if o.Metadata == nil {
			o.Metadata = make(map[string]string, len(metadata))
		}
		maps.Copy(o.Metadata, metadata)
	}
}

// WithInline はブラウザでのインライン再生を指示します。
func WithInline() WriteOption {
	return func(o *WriteOptions) { o.ContentDisposition = "inline" }
}

// WithAttachment はダウンロードを強制し、ファイル名を指定します。
func WithAttachment(filename string) WriteOption {
	return func(o *WriteOptions) {
		if filename == "" {
			o.ContentDisposition = "attachment"
			return
		}
		o.ContentDisposition = mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	}
}

// WithIfNotExists は、対象が存在しない場合にのみ書き込むことを指示します。
//
// Exists で確かめてから Write する形は、その 2 呼び出しの間に他のプロセスが
// 書き込めるため競合します。GCS の前提条件と S3 の If-None-Match をそのまま
// 使えるようにして、判定をストレージ側へ寄せます。
func WithIfNotExists() WriteOption {
	return func(o *WriteOptions) { o.IfNotExists = true }
}
