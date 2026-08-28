// Package s3 は、Amazon S3 向けの remoteio.Handler と
// そのライフサイクルを管理するファクトリを提供します。
package s3

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/shouni/go-remote-io/remoteio"
)

// DefaultRegion は、リージョンが解決できなかった場合に適用されるリージョンです。
const DefaultRegion = "ap-northeast-1"

// ClientFactory は S3 クライアントのライフサイクルを持ちます。
//
// Close と各アクセサは並行に呼ばれても安全です。
type ClientFactory struct {
	mu        sync.RWMutex
	client    *s3.Client
	awsConfig aws.Config
}

var _ remoteio.Factory = (*ClientFactory)(nil)

// settings は Option を解決した結果です。
type settings struct {
	client       *s3.Client
	awsConfig    *aws.Config
	region       string
	endpoint     string
	usePathStyle bool
	configOpts   []func(*config.LoadOptions) error
	s3Opts       []func(*s3.Options)
}

// Option は ClientFactory の生成方法を変える Functional Option です。
//
// 以前は config.LoadDefaultConfig(ctx) 決め打ちだったため、S3 互換ストレージ
// (MinIO, Cloudflare R2 など) やカスタムエンドポイントへ接続する手段が無く、
// ファクトリを使うか自前で Router を組むかの二択になっていました。
type Option func(*settings)

// WithClient は生成済みの S3 クライアントを使います。
// このとき aws.Config は解決されないため、awsConfig はゼロ値のままです。
func WithClient(client *s3.Client) Option {
	return func(s *settings) { s.client = client }
}

// WithConfig は解決済みの aws.Config を使い、LoadDefaultConfig を呼びません。
func WithConfig(cfg aws.Config) Option {
	return func(s *settings) { s.awsConfig = &cfg }
}

// WithRegion はリージョンを明示します。環境や設定ファイルより優先されます。
func WithRegion(region string) Option {
	return func(s *settings) { s.region = region }
}

// WithEndpoint は接続先エンドポイントを差し替えます。
// MinIO や Cloudflare R2 のような S3 互換ストレージ、およびテスト用のフェイク向けです。
func WithEndpoint(endpoint string) Option {
	return func(s *settings) { s.endpoint = endpoint }
}

// WithPathStyle はパス形式のアドレッシングを使います。
// 仮想ホスト形式のバケット名を解決できない S3 互換ストレージで必要になります。
func WithPathStyle() Option {
	return func(s *settings) { s.usePathStyle = true }
}

// WithConfigOptions は config.LoadDefaultConfig へ渡すオプションを指定します
// （認証情報プロバイダの差し替えなど）。
func WithConfigOptions(opts ...func(*config.LoadOptions) error) Option {
	return func(s *settings) { s.configOpts = append(s.configOpts, opts...) }
}

// WithS3Options は s3.NewFromConfig へ渡すオプションを指定します。
// WithEndpoint / WithPathStyle より後に適用されるため、必要なら上書きできます。
func WithS3Options(opts ...func(*s3.Options)) Option {
	return func(s *settings) { s.s3Opts = append(s.s3Opts, opts...) }
}

// New は新しい ClientFactory インスタンスを作成します。
// オプションを渡さない場合は IAMロールや環境変数から設定を自動検索します。
func New(ctx context.Context, opts ...Option) (*ClientFactory, error) {
	var cfg settings
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	if cfg.client != nil {
		return &ClientFactory{client: cfg.client}, nil
	}

	awsCfg, err := resolveAWSConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return &ClientFactory{
		client:    s3.NewFromConfig(awsCfg, clientOptions(cfg)...),
		awsConfig: awsCfg,
	}, nil
}

// resolveAWSConfig は aws.Config を決定します。
// 明示指定 > 環境や設定ファイル > DefaultRegion の順でリージョンが決まります。
func resolveAWSConfig(ctx context.Context, cfg settings) (aws.Config, error) {
	awsCfg := aws.Config{}
	if cfg.awsConfig != nil {
		awsCfg = *cfg.awsConfig
	} else {
		loaded, err := config.LoadDefaultConfig(ctx, cfg.configOpts...)
		if err != nil {
			return aws.Config{}, fmt.Errorf("AWS設定のロードに失敗しました (認証情報が不足しています): %w", err)
		}
		awsCfg = loaded
	}

	if cfg.region != "" {
		awsCfg.Region = cfg.region
	}
	if awsCfg.Region == "" {
		awsCfg.Region = DefaultRegion
	}
	return awsCfg, nil
}

// clientOptions は s3.NewFromConfig へ渡すオプションを組み立てます。
func clientOptions(cfg settings) []func(*s3.Options) {
	opts := make([]func(*s3.Options), 0, len(cfg.s3Opts)+2)
	if cfg.endpoint != "" {
		opts = append(opts, func(o *s3.Options) { o.BaseEndpoint = aws.String(cfg.endpoint) })
	}
	if cfg.usePathStyle {
		opts = append(opts, func(o *s3.Options) { o.UsePathStyle = true })
	}
	return append(opts, cfg.s3Opts...)
}

// Close はクライアントへの参照を手放します。冪等です。
//
// aws-sdk-go-v2 の s3.Client は明示的な Close を必要としませんが、
// クローズ後のアクセサが ErrClosed を返す契約を GCS 側と揃えています。
func (f *ClientFactory) Close() error {
	f.mu.Lock()
	f.client = nil
	f.mu.Unlock()
	return nil
}

// Handler は s3:// を担当するハンドラを返します。
//
// 複数のクラウドを 1 つの Store で扱いたい場合は、各ファクトリから取り出した
// ハンドラを remoteio.NewStore へ並べてください。
func (f *ClientFactory) Handler() (remoteio.Handler, error) {
	client, err := f.s3Client()
	if err != nil {
		return nil, err
	}
	return NewHandler(client), nil
}

// Store は s3:// とローカル関連のパスを扱う Store を返します
// （gs:// は登録されないため明確に未対応となります）。
func (f *ClientFactory) Store() (remoteio.Store, error) {
	handler, err := f.Handler()
	if err != nil {
		return nil, err
	}
	return remoteio.NewStore(handler), nil
}

// s3Client はクライアントが存命かを確かめて返します。
func (f *ClientFactory) s3Client() (*s3.Client, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.client == nil {
		return nil, fmt.Errorf("S3クライアントは初期化されていないか、既にクローズされています: %w", remoteio.ErrClosed)
	}
	return f.client, nil
}
