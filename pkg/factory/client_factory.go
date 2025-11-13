package factory

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"

	// 独立したリポジトリをインポート
	"github.com/shouni/go-remote-io/pkg/remoteio"
)

// ClientFactory は、ストレージクライアントとI/Oコンポーネントを生成するためのファクトリです。
type ClientFactory struct {
	// GCSクライアントを保持し、再利用します。
	gcsClient *storage.Client
}

// NewClientFactory は新しい ClientFactory インスタンスを作成します。
// GCSクライアントは一度だけ初期化され、ファクトリ内で再利用されます。
func NewClientFactory(ctx context.Context) (*ClientFactory, error) {
	// クライアントの初期化はここで一度だけ行われます。
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}

	// ファクトリ構造体に注入
	return &ClientFactory{gcsClient: client}, nil
}

// Close は、ファクトリが保持するGCSクライアントをクローズします。
func (f *ClientFactory) Close() error {
	if f.gcsClient != nil {
		return f.gcsClient.Close()
	}
	return nil
}

// GetGCSClient は、ファクトリが保持するGCSクライアントを返します。
func (f *ClientFactory) GetGCSClient() *storage.Client {
	return f.gcsClient
}

// GetRemoteInputReader は、GCSクライアントを注入した InputReader の具象実装を返します。
// これは、ローカルファイルとGCSの両方を扱う remoteio.LocalGCSInputReader を生成します。
func (f *ClientFactory) GetRemoteInputReader() remoteio.InputReader {
	// 保持しているクライアントを注入 (再利用)
	return remoteio.NewLocalGCSInputReader(f.gcsClient)
}

// GetGCSOutputWriter は、GCSクライアントを注入した GCSOutputWriter の具象実装を返します。
func (f *ClientFactory) GetGCSOutputWriter() remoteio.GCSOutputWriter {
	// 保持しているクライアントを注入 (再利用)
	return remoteio.NewGCSFileWriter(f.gcsClient)
}
