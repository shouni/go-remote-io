package remoteio

import (
	"context"
	"io"
	"time"
)

// IOFactory は、読み込み・書き込み・署名付きURL生成の各コンポーネントを提供します。
type IOFactory interface {
	io.Closer
	InputReader() (InputReader, error)
	OutputWriter() (OutputWriter, error)
	URLSigner() (URLSigner, error)
}

// Reader は単一のリソースを読み込む機能に特化します
type Reader interface {
	// Open は、指定されたパスから io.ReadCloser を返します。
	Open(ctx context.Context, path string) (io.ReadCloser, error)
}

// Writer は単一のリソースを書き込む機能に特化します
type Writer interface {
	// Write は、指定された path に応じて GCS、S3、またはローカルファイルへデータを書き込みます。
	Write(ctx context.Context, path string, contentReader io.Reader, contentType string) error
}

// URLSigner は、リモートストレージの署名付きURLを生成する機能を提供します。
type URLSigner interface {
	GenerateSignedURL(ctx context.Context, path string, method string, expires time.Duration) (string, error)
}

// Lister はリソースの一覧を取得する機能に特化します
type Lister interface {
	// List は、指定されたプレフィックス配下の各ファイルパスに対して callback を実行します。
	// GCS/S3 は指定 prefix に一致する全オブジェクトを返します。
	// ローカルパスの場合、指定されたディレクトリ直下のファイルのみを返し、再帰的な探索は行いません。
	// callback がエラーを返した場合、リスト処理は中断され、そのエラーが返されます。
	List(ctx context.Context, path string, callback func(path string) error) error
}

// Remover は、リソースの削除機能に特化します。
type Remover interface {
	// Delete は、指定された path のリソース（GCS/S3/ローカル）を削除します。
	Delete(ctx context.Context, path string) error
}

// Exister は、リソースの存在確認を行う機能を提供します。
type Exister interface {
	// Exists は、指定された path にリソースが存在するかどうかを確認します。
	Exists(ctx context.Context, path string) (bool, error)
}

// InputReader は、ローカルファイルパスまたはリモートURIから読み取りストリームを開き、一覧を取得するためのインターフェースを定義します。
type InputReader interface {
	Reader
	Lister
	Exister
}

// OutputWriter は書き込みに関する複合インターフェース
type OutputWriter interface {
	Writer
	Remover
}
