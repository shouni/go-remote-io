// Package gcs は、Google Cloud Storage 向けの remoteio.Handler と
// そのライフサイクルを管理するファクトリを提供します。
package gcs

import (
	"context"
	"fmt"
	"sync"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"

	"github.com/shouni/go-remote-io/remoteio"
)

// ClientFactory は、GCS クライアントのライフサイクルを持ちます。
//
// Close と各アクセサは並行に呼ばれても安全です。v1 はクライアントのフィールドを
// 無同期で nil にしていたため、Close と読み出しが競合していました
// （CI は -race 付きですが、並行に触るテストが無く検出されていませんでした）。
type ClientFactory struct {
	mu     sync.RWMutex
	client *storage.Client
	// ownsClient は、Close でクライアントを閉じてよいかを表します。
	// WithClient で注入されたクライアントのライフサイクルは呼び出し元にあります。
	ownsClient bool
}

var _ remoteio.Factory = (*ClientFactory)(nil)

// settings は Option を解決した結果です。
type settings struct {
	client     *storage.Client
	clientOpts []option.ClientOption
}

// Option は ClientFactory の生成方法を変える Functional Option です。
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

// New は ClientFactory を作成します。
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

// Close は保持している GCS クライアントを解放します。冪等です。
// WithClient で注入されたクライアントは閉じず、参照だけを手放します。
func (f *ClientFactory) Close() error {
	f.mu.Lock()
	client, owns := f.client, f.ownsClient
	f.client = nil
	f.mu.Unlock()

	if client == nil || !owns {
		return nil
	}
	return client.Close()
}

// Handler は gs:// を担当するハンドラを返します。
//
// 複数のクラウドを 1 つの Store で扱いたい場合は、各ファクトリから取り出した
// ハンドラを remoteio.NewStore へ並べてください。v1 の MultiFactory と
// HandlerProvider は、そのために必要だった足場です。
func (f *ClientFactory) Handler() (remoteio.Handler, error) {
	client, err := f.gcsClient()
	if err != nil {
		return nil, err
	}
	return NewHandler(client), nil
}

// Store は gs:// とローカル関連のパスを扱う Store を返します
// （s3:// は登録されないため明確に未対応となります）。
func (f *ClientFactory) Store() (remoteio.Store, error) {
	handler, err := f.Handler()
	if err != nil {
		return nil, err
	}
	return remoteio.NewStore(handler), nil
}

// gcsClient はクライアントが存命かを確かめて返します。
func (f *ClientFactory) gcsClient() (*storage.Client, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.client == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているか、初期化されていません: %w", remoteio.ErrClosed)
	}
	return f.client, nil
}
