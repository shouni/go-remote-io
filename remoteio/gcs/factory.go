// Package gcs は、Google Cloud Storage 向けの remoteio.IOFactory 実装を提供します。
package gcs

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"

	"github.com/shouni/go-remote-io/remoteio"
)

// ClientFactory は、GCS関連のI/Oコンポーネントを管理します。
type ClientFactory struct {
	client *storage.Client
}

var _ remoteio.IOFactory = (*ClientFactory)(nil)

// New は ClientFactory インスタンスを作成します。
func New(ctx context.Context) (*ClientFactory, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}

	return &ClientFactory{client: client}, nil
}

// Close は保持しているGCSクライアントをクローズします。
func (f *ClientFactory) Close() error {
	if f.client != nil {
		err := f.client.Close()
		f.client = nil
		return err
	}
	return nil
}

// --- Reader / InputReader 関連 ---

// Reader は単一リソースの読み込み機能を提供します。
func (f *ClientFactory) Reader() (remoteio.Reader, error) {
	return f.InputReader()
}

// InputReader は読み込みと一覧取得の両方の機能を提供します。
func (f *ClientFactory) InputReader() (remoteio.InputReader, error) {
	return f.router()
}

// --- Writer / OutputWriter 関連 ---

// Writer は単一リソースの書き込み機能を提供します。
func (f *ClientFactory) Writer() (remoteio.Writer, error) {
	return f.OutputWriter()
}

// OutputWriter は書き込み機能を提供します。
func (f *ClientFactory) OutputWriter() (remoteio.OutputWriter, error) {
	return f.router()
}

// --- その他 ---

// URLSigner は署名付きURLの生成機能を提供します。
func (f *ClientFactory) URLSigner() (remoteio.URLSigner, error) {
	client, err := f.gcsClient()
	if err != nil {
		return nil, err
	}
	return NewURLSigner(client), nil
}

// router は gs:// とローカルパスを扱う Router を組み立てます。
// ローカルを併せて登録するのは、同じリーダーで開発時のローカルファイルも
// 読めるようにするためです（s3:// は登録されないため明確に未対応となります）。
func (f *ClientFactory) router() (*remoteio.Router, error) {
	client, err := f.gcsClient()
	if err != nil {
		return nil, err
	}
	return remoteio.NewRouter(NewHandler(client), remoteio.NewLocalHandler()), nil
}

// gcsClient は内部用のヘルパーメソッドです。
// クライアントが存命かチェックし、生のリソースを返します。
func (f *ClientFactory) gcsClient() (*storage.Client, error) {
	if f.client == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているか、初期化されていません")
	}
	return f.client, nil
}
