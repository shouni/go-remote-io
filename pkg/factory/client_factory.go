package factory

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
	"github.com/shouni/go-remote-io/pkg/remoteio"
)

// Factory インターフェースの定義
type Factory interface {
	// Client はファクトリが保持するGCSクライアントを返します。
	Client() (*storage.Client, error)
	// NewInputReader は GCSクライアントを注入した InputReader を生成します。
	NewInputReader() (remoteio.InputReader, error)
	// NewOutputWriter は GCSクライアントを注入した GCSOutputWriter を生成します。
	NewOutputWriter() (remoteio.GCSOutputWriter, error)
	// Close は保持しているリソースを解放します。
	Close() error
}

// ClientFactory は Factory インターフェースの実装
type ClientFactory struct {
	gcsClient *storage.Client
}

// NewClientFactory は新しい Factory インターフェースを返す ClientFactory インスタンスを作成します。
func NewClientFactory(ctx context.Context) (Factory, error) {
	// クライアントの初期化はここで一度だけ行われます。
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}

	// ファクトリ構造体に注入
	return &ClientFactory{gcsClient: client}, nil
}

// Close は保持しているGCSクライアントをクローズし、リソースを解放します。
// クローズに成功した場合、またはクライアントが既にnilの場合はnilを返します。
func (f *ClientFactory) Close() error {
	if f.gcsClient != nil {
		err := f.gcsClient.Close()
		f.gcsClient = nil
		return err
	}
	return nil
}

// Client は、ファクトリが保持するGCSクライアントを返します。
func (f *ClientFactory) Client() (*storage.Client, error) {
	if f.gcsClient == nil {
		// クライアントがnilの場合、NewClientFactoryの失敗、またはClose()が呼び出されたことを意味する
		return nil, fmt.Errorf("GCSクライアントは既にクローズされています")
	}
	return f.gcsClient, nil
}

// NewInputReader は、GCSクライアントを注入した InputReader の具象実装を返します。
func (f *ClientFactory) NewInputReader() (remoteio.InputReader, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているため、InputReaderを生成できません")
	}
	return remoteio.NewLocalGCSInputReader(f.gcsClient), nil
}

// NewOutputWriter は、GCSクライアントを注入した GCSOutputWriter の具象実装を返します。
func (f *ClientFactory) NewOutputWriter() (remoteio.GCSOutputWriter, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているため、GCSOutputWriterを生成できません")
	}
	return remoteio.NewGCSFileWriter(f.gcsClient), nil
}
