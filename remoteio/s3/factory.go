package s3

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/shouni/go-remote-io/remoteio"
)

// S3ClientFactory は remoteio.IOFactory インターフェースを実装し、
// AWS/S3クライアントとS3関連のI/Oコンポーネントを管理します。
type S3ClientFactory struct {
	client    *s3.Client
	awsConfig aws.Config
}

// New は新しい S3ClientFactory インスタンスを作成し、remoteio.IOFactory として返します。
func New(ctx context.Context) (remoteio.IOFactory, error) {
	// 1. AWS Config のロード (IAMロール、環境変数などを自動検索)
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("AWS設定のロードに失敗しました (認証情報が不足しています): %w", err)
	}

	// リージョンが未設定の場合はデフォルト値を適用
	const defaultRegion = "ap-northeast-1"
	if awsCfg.Region == "" {
		awsCfg.Region = defaultRegion
	}

	// 2. S3 クライアントの初期化とファクトリの生成
	return &S3ClientFactory{
		client:    s3.NewFromConfig(awsCfg),
		awsConfig: awsCfg,
	}, nil
}

// Close はインターフェース要件に準拠するために実装された no-op メソッドです。
func (f *S3ClientFactory) Close() error {
	// aws-sdk-go-v2 の s3.Client は Close 不要なため、フィールドを nil にして安全を確保します。
	f.client = nil
	return nil
}

// --- Reader / InputReader 関連 ---

// Reader は単一リソースの読み込み機能を提供します。
func (f *S3ClientFactory) Reader() (remoteio.Reader, error) {
	return f.InputReader()
}

// InputReader は、S3クライアントを注入した InputReader を生成します。
func (f *S3ClientFactory) InputReader() (remoteio.InputReader, error) {
	client, err := f.s3Client()
	if err != nil {
		return nil, err
	}

	// remoteio.NewUniversalInputReader を使用 (GCSクライアントはnil)
	return remoteio.NewUniversalInputReader(nil, client), nil
}

// --- Writer / OutputWriter 関連 ---

// Writer は単一リソースの書き込み機能を提供します。
func (f *S3ClientFactory) Writer() (remoteio.Writer, error) {
	return f.OutputWriter()
}

// OutputWriter は、S3クライアントを注入した OutputWriter を生成します。
func (f *S3ClientFactory) OutputWriter() (remoteio.OutputWriter, error) {
	client, err := f.s3Client()
	if err != nil {
		return nil, err
	}

	// remoteio.NewUniversalIOWriter を使用 (GCSクライアントはnil)
	return remoteio.NewUniversalIOWriter(nil, client), nil
}

// --- その他 ---

// URLSigner は、S3クライアントを注入した URLSigner の具象実装を返します。
func (f *S3ClientFactory) URLSigner() (remoteio.URLSigner, error) {
	client, err := f.s3Client()
	if err != nil {
		return nil, err
	}
	return remoteio.NewS3URLSigner(client), nil
}

// s3Client は、ファクトリが保持するS3クライアントを返します（内部用）。
func (f *S3ClientFactory) s3Client() (*s3.Client, error) {
	if f.client == nil {
		return nil, fmt.Errorf("S3クライアントは初期化されていないか、既にクローズされています")
	}
	return f.client, nil
}
