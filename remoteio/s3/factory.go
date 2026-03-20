package s3

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/shouni/go-remote-io/remoteio"
)

// S3ClientFactory は Factory インターフェースを実装し、
// AWS/S3クライアントとS3関連のI/Oコンポーネントを管理します。
type S3ClientFactory struct {
	s3Client  *s3.Client
	awsConfig aws.Config
}

// New は新しい S3ClientFactory インスタンスを作成します。
// sync.Once を削除し、初期化ロジックを直接実行します。
func New(ctx context.Context) (remoteio.IOFactory, error) {
	// 1. AWS Config のロード (IAMロール、環境変数などを自動検索)
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("AWS設定のロードに失敗しました (認証情報が不足しています): %w", err)
	}

	const defaultRegion = "ap-northeast-1"
	if awsCfg.Region == "" {
		awsCfg.Region = defaultRegion
	}

	// 2. S3 クライアントの初期化とファクトリの生成
	return &S3ClientFactory{
		s3Client:  s3.NewFromConfig(awsCfg),
		awsConfig: awsCfg,
	}, nil
}

// Close インターフェース要件に準拠するために実装されたno-opメソッドです。
func (f *S3ClientFactory) Close() error {
	// aws-sdk-go-v2 の s3.Client は基本的に Close 不要。
	// インターフェース統一のため no-op で実装する。
	return nil
}

// InputReader は、S3クライアントのみを注入した InputReader を生成します。
// GCSクライアントはnilを渡します。
func (f *S3ClientFactory) InputReader() (remoteio.InputReader, error) {
	s3Client, err := f.getS3Client()
	if err != nil {
		return nil, fmt.Errorf("InputReaderを生成できません: S3クライアントの初期化に失敗しました")
	}

	// remoteio.NewUniversalInputReader を使用 (GCSクライアントはnil)
	return remoteio.NewUniversalInputReader(nil, s3Client), nil
}

// OutputWriter は、S3クライアントのみを注入した OutputWriter を生成します。
// GCSクライアントはnilを渡します。
func (f *S3ClientFactory) OutputWriter() (remoteio.OutputWriter, error) {
	s3Client, err := f.getS3Client()
	if err != nil {
		return nil, fmt.Errorf("OutputWriterを生成できません: S3クライアントの初期化に失敗しました")
	}

	// remoteio.NewUniversalIOWriter を使用 (GCSクライアントはnil)
	return remoteio.NewUniversalIOWriter(nil, s3Client), nil
}

// URLSigner は、S3クライアントを注入した URLSigner の具象実装を返します。
func (f *S3ClientFactory) URLSigner() (remoteio.URLSigner, error) {
	client, err := f.getS3Client()
	if err != nil {
		return nil, fmt.Errorf("S3 URLSignerを生成できません: %w", err)
	}
	// remoteio.NewS3URLSigner を使用
	return remoteio.NewS3URLSigner(client), nil
}

// getS3Client は、ファクトリが保持するS3クライアントを返します。
func (f *S3ClientFactory) getS3Client() (*s3.Client, error) {
	if f.s3Client == nil {
		return nil, fmt.Errorf("S3クライアントは初期化に失敗しています")
	}
	return f.s3Client, nil
}
