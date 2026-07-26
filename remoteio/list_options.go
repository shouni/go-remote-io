package remoteio

import "strings"

// listConfig は一覧取得の設定値を保持します。
type listConfig struct {
	delimiter string
}

// ListOption は List の挙動を変える Functional Option です。
type ListOption func(*listConfig)

// WithDelimiter は階層の区切り文字を指定し、prefix 直下のみを対象にします。
//
// 指定すると、prefix 直下のオブジェクトに加えて「疑似ディレクトリ」が列挙されます。
// 疑似ディレクトリは区切り文字で終わる URI として callback へ渡されるため、
// 呼び出し側は末尾を見て区別できます。
//
//	gs://bucket/music/20260501-abcd/   ← 疑似ディレクトリ
//	gs://bucket/music/README.md        ← 直下のオブジェクト
//
// これが要るのは、ジョブ ID のようなキーの一覧が欲しい場面です。区切り文字なしでは
// 配下の全オブジェクトが返るため、1 ジョブに 3 つ成果物があれば 3 倍のデータを受け取って
// 呼び出し側で重複を潰すことになります。区切り文字はその走査をサーバー側へ寄せます。
func WithDelimiter(delimiter string) ListOption {
	return func(c *listConfig) {
		c.delimiter = delimiter
	}
}

// newListConfig はオプションを畳み込んで設定を組み立てます。
func newListConfig(opts []ListOption) *listConfig {
	cfg := &listConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// listPrefix は、区切り文字を指定した一覧のためにプレフィックスを正規化します。
//
// 区切り文字を指定した一覧は「ディレクトリの中身を見る」操作なので、`music` と `music/` が
// 同じ結果になると困ります。前者のままだと `music-archive/` まで拾ってしまうためです。
func listPrefix(prefix string, cfg *listConfig) string {
	if cfg.delimiter == "" || prefix == "" {
		return prefix
	}
	if strings.HasSuffix(prefix, cfg.delimiter) {
		return prefix
	}
	return prefix + cfg.delimiter
}
