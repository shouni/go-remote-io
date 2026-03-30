package gcs

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
	"github.com/shouni/go-remote-io/remoteio"
)

// GCSClientFactory は remoteio.IOFactory インターフェースを実装します。
type GCSClientFactory struct {
	gcsClient *storage.Client
}

// New は GCSClientFactory インスタンスを作成し、remoteio.IOFactory として返します。
func New(ctx context.Context) (remoteio.IOFactory, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}

	return &GCSClientFactory{gcsClient: client}, nil
}

// Close は保持しているGCSクライアントをクローズします。
func (f *GCSClientFactory) Close() error {
	if f.gcsClient != nil {
		err := f.gcsClient.Close()
		f.gcsClient = nil
		return err
	}
	return nil
}

// --- Reader / InputReader 関連 ---

// Reader は単一リソースの読み込み機能を提供します。
func (f *GCSClientFactory) Reader() (remoteio.Reader, error) {
	// InputReader は Reader を満たしているため、共通の生成メソッドを呼び出します。
	return f.InputReader()
}

// InputReader は読み込みと一覧取得の両方の機能を提供します。
func (f *GCSClientFactory) InputReader() (remoteio.InputReader, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされています")
	}
	// 第2引数は S3 クライアント（今回は nil）
	return remoteio.NewUniversalInputReader(f.gcsClient, nil), nil
}

// --- Writer / OutputWriter 関連 ---

// Writer は単一リソースの書き込み機能を提供します。
func (f *GCSClientFactory) Writer() (remoteio.Writer, error) {
	// OutputWriter は Writer を満たしているため、共通の生成メソッドを呼び出します。
	return f.OutputWriter()
}

// OutputWriter は書き込み（将来的な拡張を含む）機能を提供します。
func (f *GCSClientFactory) OutputWriter() (remoteio.OutputWriter, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされています")
	}
	return remoteio.NewUniversalIOWriter(f.gcsClient, nil), nil
}

// --- その他 ---

// URLSigner は署名付きURLの生成機能を提供します。
func (f *GCSClientFactory) URLSigner() (remoteio.URLSigner, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされています")
	}
	return remoteio.NewGCSURLSigner(f.gcsClient), nil
}

// getGCSClient は内部用のヘルパーメソッドです。
func (f *GCSClientFactory) getGCSClient() (*storage.Client, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされています")
	}
	return f.gcsClient, nil
}
