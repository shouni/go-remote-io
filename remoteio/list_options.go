package remoteio

import "strings"

// ListSettings は ListOption を解決した結果です。
//
// 公開しているのは、Lister を実装する側がオプションを解釈できるようにするためです。
// 設定を非公開にすると、インターフェース経由でオプションを受け取っておきながら
// その中身を読む手段が無いという状態になり、代替実装やテストのフェイクが
// 区切り文字を無視するしかなくなります。
type ListSettings struct {
	// Delimiter は階層の区切り文字です。空文字なら prefix 配下を再帰的に列挙します。
	Delimiter string
}

// ListOption は List の挙動を変える Functional Option です。
type ListOption func(*ListSettings)

// WithDelimiter は階層の区切り文字を指定し、prefix 直下のみを対象にします。
//
// 指定すると、prefix 直下のオブジェクトに加えて「疑似ディレクトリ」が列挙されます。
// 疑似ディレクトリは区切り文字で終わる URI として callback へ渡されるため、
// 呼び出し側は末尾を見て区別できます。
//
//	gs://bucket/data/2026-05-01/   ← 疑似ディレクトリ
//	gs://bucket/data/README.md        ← 直下のオブジェクト
//
// これが要るのは、キーの一覧が欲しい場面です。区切り文字なしでは
// 配下の全オブジェクトが返るため、1 ジョブに 3 つ成果物があれば 3 倍のデータを受け取って
// 呼び出し側で重複を潰すことになります。区切り文字はその走査をサーバー側へ寄せます。
func WithDelimiter(delimiter string) ListOption {
	return func(s *ListSettings) {
		s.Delimiter = delimiter
	}
}

// NewListSettings はオプションを畳み込んで設定を組み立てます。
// Lister の実装側が受け取った opts を解釈するためにも使います。
func NewListSettings(opts ...ListOption) ListSettings {
	var settings ListSettings
	for _, opt := range opts {
		if opt != nil {
			opt(&settings)
		}
	}
	return settings
}

// ListPrefix は、区切り文字を指定した一覧のためにプレフィックスを正規化します。
//
// 区切り文字を指定した一覧は「ディレクトリの中身を見る」操作なので、`data` と `data/` が
// 同じ結果になると困ります。前者のままだと `data-archive/` まで拾ってしまうためです。
// 実装側が同じ正規化を再現できるよう公開しています。
func ListPrefix(prefix string, settings ListSettings) string {
	if settings.Delimiter == "" || prefix == "" {
		return prefix
	}
	if strings.HasSuffix(prefix, settings.Delimiter) {
		return prefix
	}
	return prefix + settings.Delimiter
}
