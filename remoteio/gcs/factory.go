// Package gcs は、Google Cloud Storage 向けの remoteio.IOFactory 実装を提供します。
package gcs

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"

	"github.com/shouni/go-remote-io/remoteio"
)

// ClientFactory は、GCS関連のI/Oコンポーネントを管理します。
type ClientFactory struct {
	client *storage.Client
	// ownsClient は、Close でクライアントを閉じてよいかを表します。
	// WithClient で注入されたクライアントのライフサイクルは呼び出し元にあります。
	ownsClient bool
}

var _ remoteio.IOFactory = (*ClientFactory)(nil)

// settings は Option を解決した結果です。
type settings struct {
	client     *storage.Client
	clientOpts []option.ClientOption
}

// Option は ClientFactory の生成方法を変える Functional Option です。
//
// 以前は storage.NewClient(ctx) 決め打ちだったため、エミュレータへの接続や
// 認証情報の差し替え、生成済みクライアントの再利用ができず、
// ファクトリを使うか自前で Router を組むかの二択になっていました。
type Option func(*settings)

// WithClient は生成済みの GCS クライアントを使います。
//
// このクライアントのライフサイクルは呼び出し元に残り、ClientFactory.Close は
// 閉じません（閉じる主体が 2 つあると、どちらが所有しているのか呼び出し側から
// 分からなくなるためです）。
func WithClient(client *storage.Client) Option {
	return func(s *settings) { s.client = client }
}

// WithClientOptions は storage.NewClient へ渡すオプションを指定します。
// エミュレータの指定 (option.WithEndpoint) や認証情報の差し替えに使います。
func WithClientOptions(opts ...option.ClientOption) Option {
	return func(s *settings) { s.clientOpts = append(s.clientOpts, opts...) }
}

// New は ClientFactory インスタンスを作成します。
// オプションを渡さない場合は Application Default Credentials でクライアントを生成します。
func New(ctx context.Context, opts ...Option) (*ClientFactory, error) {
	var cfg settings
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	if cfg.client != nil {
		return &ClientFactory{client: cfg.client, ownsClient: false}, nil
	}

	client, err := storage.NewClient(ctx, cfg.clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}

	return &ClientFactory{client: client, ownsClient: true}, nil
}

// Close は保持しているGCSクライアントをクローズします。
// WithClient で注入されたクライアントは閉じず、参照だけを手放します。
func (f *ClientFactory) Close() error {
	client := f.client
	f.client = nil
	if client == nil || !f.ownsClient {
		return nil
	}
	return client.Close()
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

// SchemeHandler は gs:// を担当するハンドラを返します。
// remoteio.NewMultiFactory が複数スキームを 1 つの Router に集約するために使います。
func (f *ClientFactory) SchemeHandler() (remoteio.SchemeHandler, error) {
	client, err := f.gcsClient()
	if err != nil {
		return nil, err
	}
	return NewHandler(client), nil
}

// router は gs:// とローカル関連のパスを扱う Router を組み立てます
// （s3:// は登録されないため明確に未対応となります）。
func (f *ClientFactory) router() (*remoteio.Router, error) {
	handler, err := f.SchemeHandler()
	if err != nil {
		return nil, err
	}
	return remoteio.NewSchemeRouter(handler), nil
}

// gcsClient は内部用のヘルパーメソッドです。
// クライアントが存命かチェックし、生のリソースを返します。
func (f *ClientFactory) gcsClient() (*storage.Client, error) {
	if f.client == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているか、初期化されていません")
	}
	return f.client, nil
}
