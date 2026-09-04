// Package remoteio は、ローカルファイル・GCS・S3 を単一のインターフェースで扱う
// ライブラリです。
//
// 入口は Store です。gcs.New / s3.New が返すファクトリから取り出すか、
// NewStore へ Handler を並べて自分で組み立てます。
//
//	factory, err := gcs.New(ctx)
//	defer factory.Close()
//	store, err := factory.Store()
//
//	jobs := store.Sub("gs://my-bucket/jobs")   // 起動時に一度だけスコープを決める
//	err = jobs.Write(ctx, jobID+"/status.json", body, remoteio.WithContentType("application/json"))
//
// 新しいストレージへ広げるときに実装するのは Handler 1 本だけです。
//
// # リモート URI は URL ではありません
//
// gs:// と s3:// は「スキーム + バケット + 生のキー」で、URL ではありません。
// オブジェクト名は任意のバイト列で、空白も ? も正当な文字ですが、net/url を通すと
// それぞれ %20 とクエリ区切りに化けます。ParseURI はデコードしないため、化けた URI は
// エラーにならないまま別のキーを指します。パスの操作には net/url も filepath も使わず、
// このパッケージの Dir / Join / BuildURI を通してください。
package remoteio

import (
	"bytes"
	"context"
	"io"
	"iter"
	"time"
)

// Reader は単一のリソースを読み込む機能に特化した、利用側向けの最小インターフェースです。
//
// 依存を絞りたい関数はこちらを受け取ってください。Store 全体を要求すると、
// 呼び出し側のテストが使いもしない操作まで用意することになります。
type Reader interface {
	// Open は、指定されたパスから io.ReadCloser を返します。
	// 対象が存在しない場合、エラーは ErrNotExist を含みます。
	Open(ctx context.Context, name string) (io.ReadCloser, error)
}

// Writer は単一のリソースを書き込む機能に特化した、利用側向けの最小インターフェースです。
type Writer interface {
	// Write は、指定されたパスへ src の内容を書き込みます。
	// 成功しなければ書き込み先は変化しません。
	Write(ctx context.Context, name string, src io.Reader, opts ...WriteOption) error
}

// Store は、ひとつのスコープに対する読み書きの窓口です。
//
// ルートの Store（NewStore の戻り値）は完全な URI とローカルパスを受け取ります。
// Sub で得たストアはプレフィックスに固定され、そこからの相対名だけを受け取ります。
//
// 読み・書き・署名を 1 つにまとめているのは、利用側がほぼ必ず 3 つとも要るためです。
// 別々のインターフェースにすると、それらを束ねるだけの型を利用側が持つことになります。
type Store interface {
	Reader
	Writer

	// Stat はメタデータを返します。対象が無ければエラーは ErrNotExist を含みます。
	Stat(ctx context.Context, name string) (ObjectInfo, error)

	// Exists は対象の有無を返します。不在は (false, nil) です。
	//
	// 「オブジェクトが在るか」だけを見ます。ローカルのディレクトリも
	// リモートの疑似ディレクトリも対象になりません。リモートにディレクトリという
	// 実体は無いため、ローカルだけ真を返すと同じ呼び出しがスキームによって
	// 別の意味になります。階層の有無を知りたい場合は List を使ってください。
	Exists(ctx context.Context, name string) (bool, error)

	// List は name 配下を列挙します。プレフィックスは常に「その階層の中身」を指す形へ
	// 正規化されるため、"data" と "data/" は同じ結果になり、"data-archive/" は
	// 一致しません。
	//
	// 何も無いプレフィックスの一覧は、エラーではなく空で返ります。リモートに
	// 「存在しないプレフィックス」という状態が無いためで、ローカルだけエラーにすると
	// 同じ呼び出しがスキームによって別の意味になります。ディレクトリの有無を
	// 確かめる用途には使えません。
	//
	// 区切り文字を渡さない一覧は、プレフィックス配下を再帰的に返します。1 ジョブに
	// 成果物が 3 つあれば 3 倍のデータを受け取り、呼び出し側で重複を潰すことに
	// なります。直下だけでよいなら WithDelimiter("/") でその走査をサーバー側へ
	// 寄せてください。
	//
	//	for entry, err := range store.List(ctx, "jobs", remoteio.WithDelimiter("/")) {
	//		if err != nil {
	//			return err
	//		}
	//		if entry.IsPrefix {
	//			...
	//		}
	//	}
	List(ctx context.Context, name string, opts ...ListOption) iter.Seq2[Entry, error]

	// Delete は対象を削除します。不在はエラーにしません（冪等）。
	Delete(ctx context.Context, name string) error

	// Copy は src の内容を dst へ複製します。スキームは跨げます。
	//
	// 両者が同じハンドラに解決され、そのハンドラが Copier を実装していれば
	// サーバーサイドコピーになります。そうでなければストリームで中継します。
	// 呼び出し側に分岐は要りません。
	Copy(ctx context.Context, src, dst string, opts ...WriteOption) error

	// SignURL は署名付き URL を生成します。
	// 担当ハンドラが Signer を実装していない場合、エラーは ErrNotSupported を含みます。
	SignURL(ctx context.Context, name, method string, expires time.Duration) (string, error)

	// Sub は、プレフィックスに固定されたストアを返します。
	//
	// 呼び出しのたびにバケット名を連れ回す代わりに、組み立て時に一度だけ決めます。
	// 得られたストアはスキーム付きの絶対 URI を受け取らず、渡すと
	// ErrAbsoluteName を含むエラーになります（スコープを絞ったつもりのコードが
	// 別のバケットへ書けてしまうのを防ぐため）。
	Sub(prefix string) Store
}

// ReadAll は name の内容をすべて読み取って返します。
// Open と Close の組を毎回書かずに済ませるための補助です。
func ReadAll(ctx context.Context, r Reader, name string) ([]byte, error) {
	rc, err := r.Open(ctx, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, wrapf(err, "読み込みに失敗しました (%s)", name)
	}
	return data, nil
}

// WriteAll は data を name へ書き込みます。
func WriteAll(ctx context.Context, w Writer, name string, data []byte, opts ...WriteOption) error {
	return w.Write(ctx, name, bytes.NewReader(data), opts...)
}

// Move は Copy のあとにコピー元を削除します。
// コピーが成功した場合にのみ削除するため、途中で失敗してもコピー元は残ります。
func Move(ctx context.Context, s Store, src, dst string, opts ...WriteOption) error {
	if err := s.Copy(ctx, src, dst, opts...); err != nil {
		return err
	}
	if err := s.Delete(ctx, src); err != nil {
		return wrapf(err, "コピー元の削除に失敗しました (%s)", src)
	}
	return nil
}

// Factory は、ストレージクライアントのライフサイクルを持ち、そこから Store と
// Handler を取り出せることを表します。gcs.ClientFactory と s3.ClientFactory が
// 実装しています。
//
// Handler も返すのは、複数のクラウドを 1 つの Store へ束ねるときに担当スキームが
// 要るためです。Store だけを返す形にすると、束ねる側が「このファクトリはどのスキームか」
// を知る別の口を要求することになります。
type Factory interface {
	io.Closer

	// Store は、このファクトリのスキームとローカル関連のパスを扱う Store を返します。
	Store() (Store, error)

	// Handler は、このファクトリが担当するスキームのハンドラを返します。
	// 複数のクラウドを 1 つの Store へ束ねるときに使います。
	Handler() (Handler, error)
}
