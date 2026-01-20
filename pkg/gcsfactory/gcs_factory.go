package gcsfactory

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
	"github.com/shouni/go-remote-io/pkg/remoteio"
)

// Factory インターフェースの定義

// GCSClientFactory は Factory インターフェースを実装し、GCSクライアントと関連するI/Oコンポーネントを管理します。
type GCSClientFactory struct {
	gcsClient *storage.Client
}

// New は新しい Factory インターフェースの実装である GCSClientFactory インスタンスを作成します。
func New(ctx context.Context) (remoteio.IOFactory, error) {
	// クライアントの初期化はここで一度だけ行われます。
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}

	// ファクトリ構造体に注入
	return &GCSClientFactory{gcsClient: client}, nil
}

// Close は保持しているGCSクライアントをクローズし、リソースを解放します。
// クローズに成功した場合、またはクライアントが既にnilの場合はnilを返します。
func (f *GCSClientFactory) Close() error {
	if f.gcsClient != nil {
		err := f.gcsClient.Close()
		f.gcsClient = nil
		return err
	}
	return nil
}

// InputReader は、GCSクライアントを注入した UniversalInputReader の具象実装を返します。
func (f *GCSClientFactory) InputReader() (remoteio.InputReader, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているため、InputReaderを生成できません")
	}
	return remoteio.NewUniversalInputReader(f.gcsClient, nil), nil
}

// OutputWriter は、GCSクライアントを注入した UniversalIOWriter の具象実装を返します。
func (f *GCSClientFactory) OutputWriter() (remoteio.OutputWriter, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているため、OutputWriterを生成できません")
	}
	return remoteio.NewUniversalIOWriter(f.gcsClient, nil), nil
}

// URLSigner は、GCSクライアントを注入した URLSigner の具象実装を返します。
func (f *GCSClientFactory) URLSigner() (remoteio.URLSigner, error) {
	if f.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントは既にクローズされているため、URLSignerを生成できません")
	}
	return remoteio.NewGCSURLSigner(f.gcsClient), nil
}

// GetGCSClient は、ファクトリが保持するGCSクライアントを返します。
func (f *GCSClientFactory) getGCSClient() (*storage.Client, error) {
	if f.gcsClient == nil {
		// クライアントがnilの場合、NewGCSClientFactoryの失敗、またはClose()が呼び出されたことを意味する
		return nil, fmt.Errorf("GCSクライアントは既にクローズされています")
	}
	return f.gcsClient, nil
}
