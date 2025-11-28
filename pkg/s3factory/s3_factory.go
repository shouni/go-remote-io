package s3factory

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/shouni/go-remote-io/pkg/remoteio"
)

// Factory インターフェースの定義 (変更なし)
type Factory interface {
	GetS3Client() (*s3.Client, error)
	NewS3URLSigner() (remoteio.URLSigner, error)
	NewInputReader() (remoteio.InputReader, error)
	NewOutputWriter() (remoteio.OutputWriter, error)
}

// S3ClientFactory は Factory インターフェースを実装し、
// AWS/S3クライアントとS3関連のI/Oコンポーネントを管理します。
type S3ClientFactory struct {
	s3Client  *s3.Client
	awsConfig aws.Config
}

// NewS3ClientFactory は新しい S3ClientFactory インスタンスを作成します。
// sync.Once を削除し、初期化ロジックを直接実行します。
func NewS3ClientFactory(ctx context.Context) (Factory, error) {
	// 1. AWS Config のロード (IAMロール、環境変数などを自動検索)
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("AWS設定のロードに失敗しました (認証情報またはリージョン設定を確認してください): %w", err)
	}

	// 2. S3 クライアントの初期化とファクトリの生成
	return &S3ClientFactory{
		s3Client:  s3.NewFromConfig(awsCfg),
		awsConfig: awsCfg,
	}, nil
}

// GetS3Client は、ファクトリが保持するS3クライアントを返します。
func (f *S3ClientFactory) GetS3Client() (*s3.Client, error) {
	if f.s3Client == nil {
		return nil, fmt.Errorf("S3クライアントは初期化に失敗しています")
	}
	return f.s3Client, nil
}

// NewS3URLSigner は、S3クライアントを注入した URLSigner の具象実装を返します。
func (f *S3ClientFactory) NewS3URLSigner() (remoteio.URLSigner, error) {
	client, err := f.GetS3Client()
	if err != nil {
		return nil, fmt.Errorf("S3 URLSignerを生成できません: %w", err)
	}
	// remoteio.NewS3URLSigner を使用
	return remoteio.NewS3URLSigner(client), nil
}

// NewInputReader は、S3クライアントのみを注入した InputReader を生成します。
// GCSクライアントはnilを渡します。
func (f *S3ClientFactory) NewInputReader() (remoteio.InputReader, error) {
	s3Client, err := f.GetS3Client()
	if err != nil {
		return nil, fmt.Errorf("InputReaderを生成できません: S3クライアントの初期化に失敗しました")
	}

	// remoteio.NewUniversalInputReader を使用 (GCSクライアントはnil)
	return remoteio.NewUniversalInputReader(nil, s3Client), nil
}

// NewOutputWriter は、S3クライアントのみを注入した OutputWriter を生成します。
// GCSクライアントはnilを渡します。
func (f *S3ClientFactory) NewOutputWriter() (remoteio.OutputWriter, error) {
	s3Client, err := f.GetS3Client()
	if err != nil {
		return nil, fmt.Errorf("OutputWriterを生成できません: S3クライアントの初期化に失敗しました")
	}

	// remoteio.NewUniversalIOWriter を使用 (GCSクライアントはnil)
	return remoteio.NewUniversalIOWriter(nil, s3Client), nil
}
