package factory

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
	"github.com/shouni/go-remote-io/pkg/remoteio"
)

type Factory interface {
	GetGCSClient() (*storage.Client, error)
	GetRemoteInputReader() (remoteio.InputReader, error)
	GetGCSOutputWriter() (remoteio.GCSOutputWriter, error)
	Close() error
}

// ClientFactory は Factory インターフェースの実装
type ClientFactory struct {
	gcsClient *storage.Client
}

// NewClientFactory は新しい Factory インスタンスを作成します。
func NewClientFactory(ctx context.Context) (Factory, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}
	return &ClientFactory{gcsClient: client}, nil
}

// Close は、ファクトリが保持するGCSクライアントをクローズします。
func (f *ClientFactory) Close() error {
	if f.gcsClient != nil {
		err := f.gcsClient.Close()
		f.gcsClient = nil // クローズ後にnilに設定
		return err
	}
	return nil
}

// GetGCSClient は、ファクトリが保持するGCSクライアントを返します。
func (f *ClientFactory) GetGCSClient() (*storage.Client, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされています")
	}
	return f.gcsClient, nil
}

// GetRemoteInputReader は、GCSクライアントを注入した InputReader の具象実装を返します。
func (f *ClientFactory) GetRemoteInputReader() (remoteio.InputReader, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているため、InputReaderを生成できません")
	}
	return remoteio.NewLocalGCSInputReader(f.gcsClient), nil
}

// GetGCSOutputWriter は、GCSクライアントを注入した GCSOutputWriter の具象実装を返します。
func (f *ClientFactory) GetGCSOutputWriter() (remoteio.GCSOutputWriter, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているため、GCSOutputWriterを生成できません")
	}
	return remoteio.NewGCSFileWriter(f.gcsClient), nil
}
