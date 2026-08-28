package remoteio

import (
	"errors"
	"fmt"
	"io/fs"
)

// このパッケージが返す番兵エラーです。呼び出し側がメッセージ文字列ではなく
// errors.Is で分岐できるようにするためのものです。
//
// 番兵が無いと「未対応のURIスキームです」のような判定は文字列比較でしか書けず、
// 利用側がその文言を自前で持つことになります。そうなるとライブラリ側で
// メッセージを変えたときに、静かに壊れます。
//
// 番兵の文言は英語で "remoteio: " を接頭辞に付けます。ラップする側の説明文は
// 日本語のままです（番兵は識別子、ラップは読み手向けの文脈という切り分け）。
var (
	// ErrNotExist は、対象のオブジェクトやファイルが存在しないことを表します。
	// スキームに依らず errors.Is(err, remoteio.ErrNotExist) で判定できます。
	// io/fs と同じ値なので os.ErrNotExist とも一致します。
	ErrNotExist = fs.ErrNotExist

	// ErrExist は、WithIfNotExists を指定した書き込みで対象が既に存在したことを表します。
	// io/fs と同じ値なので os.ErrExist とも一致します。
	ErrExist = fs.ErrExist

	// ErrUnsupportedScheme は、どのハンドラも担当していない URI スキームを表します。
	ErrUnsupportedScheme = errors.New("remoteio: unsupported URI scheme")

	// ErrClosed は、既にクローズされたファクトリやストアを操作したことを表します。
	ErrClosed = errors.New("remoteio: factory is closed")

	// ErrNotSupported は、ハンドラがその操作に対応していないことを表します。
	// 署名付き URL やサーバーサイドコピーのような任意インターフェースを
	// 実装していないハンドラへ要求したときに返ります。
	ErrNotSupported = errors.New("remoteio: operation not supported by handler")

	// ErrAbsoluteName は、スコープ付きストア (Store.Sub の戻り値) へ
	// スキーム付きの絶対 URI を渡したことを表します。
	//
	// 黙って素通しすると、スコープを絞ったつもりのコードが別のバケットへ書けて
	// しまいます。絶対 URI を扱いたい場合はルートのストアを使ってください。
	ErrAbsoluteName = errors.New("remoteio: absolute URI passed to a scoped store")

	// ErrInvalidURI は、URI の形が想定と違うことを表します
	// （スキームが無い、バケット名が空、オブジェクト名が必要なのに空など）。
	ErrInvalidURI = errors.New("remoteio: invalid URI")
)

// wrapf は、番兵を保ったまま日本語の文脈を付けてエラーを包みます。
// 呼び出し側の判定は errors.Is で、読み手向けの説明は日本語で、という切り分けを
// 全ファイルで同じ形に保つための補助です。
func wrapf(err error, format string, args ...any) error {
	return fmt.Errorf(fmt.Sprintf(format, args...)+": %w", err)
}
