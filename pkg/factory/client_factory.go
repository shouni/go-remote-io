package factory

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
)

// GetGCSClient は、Google Cloud Storage (GCS) のクライアントを作成します。
// GCSクライアントは環境変数や認証情報を自動で処理します。
func GetGCSClient(ctx context.Context) (*storage.Client, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("GCSクライアントの初期化に失敗しました: %w", err)
	}

	return client, nil
}
