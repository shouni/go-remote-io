package remoteio

import (
	"context"
	"io"
	"iter"
	"time"
)

// Handler は 1 つの URI スキームに対する読み書きの実装です。
// このライブラリを新しいストレージへ広げるときに実装するのは、この 1 本だけです。
//
// v1 は拡張点が 2 系統ありました。利用側が実装する Lister / Reader / Writer …は
// 可変長オプションを受け取り、ライブラリ内部の SchemeHandler は解決済みの設定を
// 受け取る、という形です。そのために ListSettings / NewListSettings / ListPrefix /
// WriteSettings を公開せざるを得ず、しかも実際にそれらを使っていたのは
// テストのフェイクだけでした。v2 は Handler を唯一の実装対象にし、
// オプションの解決はライブラリ側で完結させています。
//
// Exists がメソッドに無いのは、Stat と errors.Is(err, ErrNotExist) から導出できるためです。
// 実装が 1 つ減り、「不在を (false, nil) で返す」という約束を取り違える余地も消えます。
// 利用側は Store.Exists を呼んでください。
type Handler interface {
	// Scheme は、このハンドラが担当するスキーム名 ("gs" など) を返します。
	// 区切り ("://") は含みません。
	//
	// 空文字を返すハンドラは、どのスキームにも一致しなかったパスを受け取る
	// フォールバック（ローカルファイルシステム）として扱われます。
	Scheme() string

	// Open は読み取りストリームを返します。
	// 対象が存在しない場合、エラーは ErrNotExist を含まなければなりません。
	Open(ctx context.Context, uri string) (io.ReadCloser, error)

	// Stat はメタデータを返します。
	// 対象が存在しない場合、エラーは ErrNotExist を含まなければなりません。
	Stat(ctx context.Context, uri string) (ObjectInfo, error)

	// List は uri 配下を列挙します。uri は ListPrefix で正規化済みの状態で渡されます。
	//
	// 反復の途中で失敗したときは、ゼロ値の Entry と共にエラーを 1 度だけ yield して
	// 打ち切ってください。呼び出し側が break したときは、そのまま反復を止めます。
	List(ctx context.Context, uri string, opts ListOptions) iter.Seq2[Entry, error]

	// Write は uri へ src の内容を書き込みます。
	//
	// 実装は「成功しなければ書き込み先が変化しない」ことを保証しなければなりません。
	// 途中で失敗した結果として切り詰められたオブジェクトが残ってはいけません。
	// ローカルは一時ファイル + rename、GCS は ctx のキャンセルによる中断、
	// S3 はマルチパートの abort でこれを満たしています。
	//
	// opts.IfNotExists が真のときは、既に存在する場合に ErrExist を含むエラーを返します。
	Write(ctx context.Context, uri string, src io.Reader, opts WriteOptions) error

	// Delete は対象を削除します。不在はエラーにしません（冪等）。
	Delete(ctx context.Context, uri string) error
}

// Copier は、同じスキーム内でサーバーサイドコピーができるハンドラが実装する
// 任意インターフェースです。
//
// Store.Copy は、コピー元とコピー先が同じハンドラに解決され、かつそのハンドラが
// Copier を実装しているときにだけこちらへ落とします。実装していなければ
// これまで通りストリームで中継するので、呼び出し側に分岐は要りません。
//
// v1 にこれが無かったため、同一バケット内のコピーでも全バイトがクライアントを
// 往復していました。利用側が storage.ObjectHandle.CopierFrom を自前で呼ぶために
// 2 個目の GCS クライアントを持つ、という迂回が実際に発生しています。
type Copier interface {
	CopyTo(ctx context.Context, src, dst string) error
}

// Signer は、署名付き URL を生成できるハンドラが実装する任意インターフェースです。
//
// v1 は URLSigner を独立したインターフェースにしていたため、複数スキームを束ねる側が
// signerRouter という専用の振り分けを別に持つ必要がありました。ハンドラの任意機能に
// することで、振り分けは Router の 1 箇所に戻ります。
type Signer interface {
	SignURL(ctx context.Context, uri, method string, expires time.Duration) (string, error)
}

// ObjectInfo は、リソースのメタデータです。
type ObjectInfo struct {
	// URI は問い合わせに使った URI（ローカルならパス）がそのまま入ります。
	URI string
	// Size はバイト数です。
	Size int64
	// ModTime は最終更新時刻です。
	ModTime time.Time
	// ContentType は MIME タイプです。ローカルファイルシステムでは空になります。
	ContentType string
	// Metadata は WithMetadata で書き込んだユーザー定義メタデータです。
	// ローカルファイルシステムでは常に nil です。
	// S3 はキーを小文字に正規化するため、書き込み時と大文字小文字が変わることがあります。
	Metadata map[string]string
}

// Entry は List が返す 1 件です。
//
// v1 の List は callback に文字列だけを渡していました。疑似ディレクトリかどうかは
// 「末尾が区切り文字か」で呼び出し側が判定する約束で、実際に利用側がその判定を
// 書いています。ハンドラは attrs.Prefix や CommonPrefixes という型のついた情報を
// 持っていたのに、文字列へ潰して渡していたことになります。
type Entry struct {
	// URI は完全な URI です。ルートの Store へそのまま渡せます。
	URI string
	// Name は列挙したプレフィックスからの相対名です。
	// スコープ付きストア (Store.Sub) へそのまま渡せます。
	Name string
	// IsPrefix は、これが疑似ディレクトリ（区切り文字で畳まれた階層）であることを表します。
	// 区切り文字を指定した一覧でのみ真になり得ます。
	IsPrefix bool
	// Size はバイト数です。IsPrefix が真のときは 0 です。
	Size int64
	// ModTime は最終更新時刻です。IsPrefix が真のときはゼロ値です。
	ModTime time.Time
}
