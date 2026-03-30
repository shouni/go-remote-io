package remoteio

import (
	"context"
	"io"
	"time"
)

// IOFactory インターフェースの定義
type IOFactory interface {
	io.Closer // Close() error
	Reader() (Reader, error)
	InputReader() (InputReader, error)
	Writer() (Writer, error)
	OutputWriter() (OutputWriter, error)
	URLSigner() (URLSigner, error)
}

// Reader は単一のリソースを読み込む機能に特化します
type Reader interface {
	// Open は、指定されたパスから io.ReadCloser を返します。
	Open(ctx context.Context, filePath string) (io.ReadCloser, error)
}

// Lister はリソースの一覧を取得する機能に特化します
type Lister interface {
	// List は、指定されたプレフィックス配下の各ファイルパスに対して callback を実行します。
	// ローカルパスの場合、指定されたディレクトリ直下のファイルのみを処理し、再帰的な探索は行いません。
	// callback がエラーを返した場合、リスト処理は中断され、そのエラーが返されます。
	List(ctx context.Context, path string, callback func(filePath string) error) error
}

// Writer は単一のリソースを書き込む機能に特化します
type Writer interface {
	// Write は、URIのプレフィックスに応じてGCS、S3、またはローカルファイルパスへデータを書き込みます。
	Write(ctx context.Context, uri string, contentReader io.Reader, contentType string) error
}

// URLSigner は、リモートストレージの署名付きURLを生成する機能を提供します。
type URLSigner interface {
	GenerateSignedURL(ctx context.Context, uri string, method string, expires time.Duration) (string, error)
}

// InputReader は、ローカルファイルパスまたはリモートURIから
// 読み取りストリームを開き、一覧を取得するためのインターフェースを定義します。
type InputReader interface {
	Reader
	Lister
}

// OutputWriter は書き込みに関する複合インターフェース
type OutputWriter interface {
	Writer
}
