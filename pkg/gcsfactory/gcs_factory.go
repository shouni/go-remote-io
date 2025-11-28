package gcsfactory

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/shouni/go-remote-io/pkg/remoteio"
)

// Factory インターフェースの定義
type Factory interface {
	// GetGCSClient はファクトリが保持するGCSクライアントを返します。
	GetGCSClient() (*storage.Client, error)
	// NewGCSURLSigner は GCSクライアントを注入した URLSigner を生成します。
	NewGCSURLSigner() (remoteio.URLSigner, error)
	// NewInputReader は GCSクライアントを注入した InputReader を生成します。
	NewInputReader() (remoteio.InputReader, error)
	// NewOutputWriter は GCSクライアントを注入した OutputWriter を生成します。
	// このファクトリから生成される OutputWriter は、GCSとローカルファイルシステムへの書き込みをサポートします。
	NewOutputWriter() (remoteio.OutputWriter, error)
	// Close は保持しているリソースを解放します。
	Close() error
}

// ClientFactory は Factory インターフェースを実装し、GCSクライアントと関連するI/Oコンポーネントを管理します。
type ClientFactory struct {
	gcsClient *storage.Client
}

// NewGCSClientFactory は新しい Factory インターフェースの実装である ClientFactory インスタンスを作成します。
// 注: このファクトリはGCSクライアントのみを初期化します。S3クライアントが必要な場合は、他のファクトリで初期化するか、
// このファクトリにS3クライアントを追加する必要があります。
func NewGCSClientFactory(ctx context.Context) (Factory, error) {
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

// GetGCSClient は、ファクトリが保持するGCSクライアントを返します。
func (f *ClientFactory) GetGCSClient() (*storage.Client, error) {
	if f.gcsClient == nil {
		// クライアントがnilの場合、NewClientFactoryの失敗、またはClose()が呼び出されたことを意味する
		return nil, fmt.Errorf("GCSクライアントは既にクローズされています")
	}
	return f.gcsClient, nil
}

// NewGCSURLSigner は、GCSクライアントを注入した URLSigner の具象実装を返します。
func (f *ClientFactory) NewGCSURLSigner() (remoteio.URLSigner, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているため、URLSignerを生成できません")
	}
	// remoteio.NewGCSURLSigner を使用
	return remoteio.NewGCSURLSigner(f.gcsClient), nil
}

// NewInputReader は、GCSクライアントを注入した UniversalInputReader の具象実装を返します。
func (f *ClientFactory) NewInputReader() (remoteio.InputReader, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているため、InputReaderを生成できません")
	}
	// 記憶した remoteio.NewUniversalInputReader を使用し、S3クライアントには nil を渡す。
	return remoteio.NewUniversalInputReader(f.gcsClient, (*s3.Client)(nil)), nil
}

// NewOutputWriter は、GCSクライアントを注入した UniversalIOWriter の具象実装を返します。
func (f *ClientFactory) NewOutputWriter() (remoteio.OutputWriter, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているため、OutputWriterを生成できません")
	}

	// 記憶した remoteio.NewUniversalIOWriter を使用し、S3クライアントには nil を渡す。
	return remoteio.NewUniversalIOWriter(f.gcsClient, (*s3.Client)(nil)), nil
}
