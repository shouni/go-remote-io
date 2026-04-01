package gcs

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"

	"github.com/shouni/go-remote-io/remoteio"
)

// GCSClientFactory は、GCS関連のI/Oコンポーネントを管理します。
type GCSClientFactory struct {
	client *storage.Client
}

// New は GCSClientFactory インスタンスを作成します。
func New(ctx context.Context) (*GCSClientFactory, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}

	return &GCSClientFactory{client: client}, nil
}

// Close は保持しているGCSクライアントをクローズします。
func (f *GCSClientFactory) Close() error {
	if f.client != nil {
		err := f.client.Close()
		f.client = nil
		return err
	}
	return nil
}

// --- Reader / InputReader 関連 ---

// Reader は単一リソースの読み込み機能を提供します。
func (f *GCSClientFactory) Reader() (remoteio.Reader, error) {
	return f.InputReader()
}

// InputReader は読み込みと一覧取得の両方の機能を提供します。
func (f *GCSClientFactory) InputReader() (remoteio.InputReader, error) {
	client, err := f.gcsClient()
	if err != nil {
		return nil, err
	}
	// 第2引数は S3 クライアント（今回は nil）
	return remoteio.NewUniversalInputReader(client, nil), nil
}

// --- Writer / OutputWriter 関連 ---

// Writer は単一リソースの書き込み機能を提供します。
func (f *GCSClientFactory) Writer() (remoteio.Writer, error) {
	return f.OutputWriter()
}

// OutputWriter は書き込み機能を提供します。
func (f *GCSClientFactory) OutputWriter() (remoteio.OutputWriter, error) {
	client, err := f.gcsClient()
	if err != nil {
		return nil, err
	}
	return remoteio.NewUniversalIOWriter(client, nil), nil
}

// --- その他 ---

// URLSigner は署名付きURLの生成機能を提供します。
func (f *GCSClientFactory) URLSigner() (remoteio.URLSigner, error) {
	client, err := f.gcsClient()
	if err != nil {
		return nil, err
	}
	return remoteio.NewGCSURLSigner(client), nil
}

// gcsClient は内部用のヘルパーメソッドです。
// クライアントが存命かチェックし、生のリソースを返します。
func (f *GCSClientFactory) gcsClient() (*storage.Client, error) {
	if f.client == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているか、初期化されていません")
	}
	return f.client, nil
}
