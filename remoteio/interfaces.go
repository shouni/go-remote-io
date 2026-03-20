package remoteio

import (
	"context"
	"io"
	"time"
)

// IOFactory インターフェースの定義
type IOFactory interface {
	io.Closer // Close() error
	InputReader() (InputReader, error)
	OutputWriter() (OutputWriter, error)
	URLSigner() (URLSigner, error)
}

// InputReader は、ローカルファイルパスまたはリモートURIから
// 読み取りストリームを開き、一覧を取得するためのインターフェースを定義します。
type InputReader interface {
	// Open は、指定されたパスから io.ReadCloser を返します。
	Open(ctx context.Context, filePath string) (io.ReadCloser, error)

	// List は、指定されたプレフィックス配下の各ファイルパスに対して callback を実行します。
	// ローカルパスの場合、指定されたディレクトリ直下のファイルのみを処理し、再帰的な探索は行いません。
	// callback がエラーを返した場合、リスト処理は中断され、そのエラーが返されます。
	List(ctx context.Context, path string, callback func(filePath string) error) error
}

// OutputWriter は、GCS、S3、およびローカルファイルシステムへの書き込みを抽象化する汎用インターフェースです。
type OutputWriter interface {
	// Write は、URIのプレフィックスに応じてGCS、S3、またはローカルファイルパスへデータを書き込みます。
	Write(ctx context.Context, uri string, contentReader io.Reader, contentType string) error
}

// URLSigner は、リモートストレージの署名付きURLを生成する機能を提供します。
type URLSigner interface {
	GenerateSignedURL(ctx context.Context, uri string, method string, expires time.Duration) (string, error)
}
