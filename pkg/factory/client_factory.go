package factory

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"

	"github.com/shouni/go-remote-io/pkg/remoteio"
)

// =================================================================
// ストレージクライアントとI/Oコンポーネントのファクトリ
// =================================================================

// GetGCSClient は、Google Cloud Storage (GCS) のクライアントを作成します。
// GCSクライアントは環境変数や認証情報を自動で処理します。
func GetGCSClient(ctx context.Context) (*storage.Client, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}
	return client, nil
}

// GetRemoteInputReader は、GCSクライアントを注入した InputReader の具象実装を返します。
// これは、ローカルファイルとGCSの両方を扱う remoteio.LocalGCSInputReader を生成します。
func GetRemoteInputReader(ctx context.Context) (remoteio.InputReader, error) {
	gcsClient, err := GetGCSClient(ctx)
	if err != nil {
		return nil, err
	}
	// remoteio パッケージの具象構造体を生成し、インターフェースとして返却
	return remoteio.NewLocalGCSInputReader(gcsClient), nil
}

// GetGCSOutputWriter は、GCSクライアントを注入した GCSOutputWriter の具象実装を返します。
func GetGCSOutputWriter(ctx context.Context) (remoteio.GCSOutputWriter, error) {
	gcsClient, err := GetGCSClient(ctx)
	if err != nil {
		return nil, err
	}
	// remoteio パッケージの具象構造体を生成し、インターフェースとして返却
	return remoteio.NewGCSFileWriter(gcsClient), nil
}
