// Package s3 provides a remoteio.Handler for Amazon S3 and a factory that manages
// its lifecycle.
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

// DefaultRegion is the region applied when no region can be resolved.
const DefaultRegion = "ap-northeast-1"

// ClientFactory owns the lifecycle of an S3 client.
//
// Close and each accessor are safe to call concurrently.
type ClientFactory struct {
	mu        sync.RWMutex
	client    *s3.Client
	awsConfig aws.Config
}

var _ remoteio.Factory = (*ClientFactory)(nil)

// settings is the result of resolving Option values.
type settings struct {
	client       *s3.Client
	awsConfig    *aws.Config
	region       string
	endpoint     string
	usePathStyle bool
	configOpts   []func(*config.LoadOptions) error
	s3Opts       []func(*s3.Options)
}

// Option is a functional option that changes how a ClientFactory is created.
//
// It used to be hard-wired to config.LoadDefaultConfig(ctx), so there was no way to
// connect to S3-compatible storage (MinIO, Cloudflare R2 and so on) or a custom
// endpoint; the choice was between using the factory and building a Router yourself.
type Option func(*settings)

// WithClient uses an already created S3 client.
// No aws.Config is resolved in that case, so awsConfig stays the zero value.
func WithClient(client *s3.Client) Option {
	return func(s *settings) { s.client = client }
}

// WithConfig uses an already resolved aws.Config and does not call LoadDefaultConfig.
func WithConfig(cfg aws.Config) Option {
	return func(s *settings) { s.awsConfig = &cfg }
}

// WithRegion sets the region explicitly. It takes precedence over the environment and config files.
func WithRegion(region string) Option {
	return func(s *settings) { s.region = region }
}

// WithEndpoint replaces the endpoint to connect to.
// It is meant for S3-compatible storage such as MinIO and Cloudflare R2, and for test fakes.
func WithEndpoint(endpoint string) Option {
	return func(s *settings) { s.endpoint = endpoint }
}

// WithPathStyle uses path-style addressing.
// It is needed for S3-compatible storage that cannot resolve virtual-hosted-style bucket names.
func WithPathStyle() Option {
	return func(s *settings) { s.usePathStyle = true }
}

// WithConfigOptions sets the options passed to config.LoadDefaultConfig
// (to swap the credentials provider, for example).
func WithConfigOptions(opts ...func(*config.LoadOptions) error) Option {
	return func(s *settings) { s.configOpts = append(s.configOpts, opts...) }
}

// WithS3Options sets the options passed to s3.NewFromConfig.
// They are applied after WithEndpoint / WithPathStyle, so they can override those if needed.
func WithS3Options(opts ...func(*s3.Options)) Option {
	return func(s *settings) { s.s3Opts = append(s.s3Opts, opts...) }
}

// New creates a new ClientFactory instance.
// Without options, configuration is discovered automatically from IAM roles and environment variables.
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

// resolveAWSConfig decides the aws.Config.
// The region is decided in the order: explicit > environment or config file > DefaultRegion.
func resolveAWSConfig(ctx context.Context, cfg settings) (aws.Config, error) {
	awsCfg := aws.Config{}
	if cfg.awsConfig != nil {
		awsCfg = *cfg.awsConfig
	} else {
		loaded, err := config.LoadDefaultConfig(ctx, cfg.configOpts...)
		if err != nil {
			return aws.Config{}, fmt.Errorf("load AWS config (missing credentials): %w", err)
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

// clientOptions builds the options passed to s3.NewFromConfig.
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

// Close drops the reference to the client. It is idempotent.
//
// The aws-sdk-go-v2 s3.Client needs no explicit Close, but the contract that
// accessors return ErrClosed after Close is kept the same as on the GCS side.
func (f *ClientFactory) Close() error {
	f.mu.Lock()
	f.client = nil
	f.mu.Unlock()
	return nil
}

// Handler returns the handler responsible for s3://.
//
// To handle several clouds in one Store, line up the handlers taken from each
// factory in remoteio.NewStore.
func (f *ClientFactory) Handler() (remoteio.Handler, error) {
	client, err := f.s3Client()
	if err != nil {
		return nil, err
	}
	return NewHandler(client), nil
}

// Store returns a Store that handles s3:// and local paths
// (gs:// is not registered, so it is clearly unsupported).
func (f *ClientFactory) Store() (remoteio.Store, error) {
	handler, err := f.Handler()
	if err != nil {
		return nil, err
	}
	return remoteio.NewStore(handler), nil
}

// s3Client checks that the client is still alive and returns it.
func (f *ClientFactory) s3Client() (*s3.Client, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.client == nil {
		return nil, fmt.Errorf("S3 client is not initialized or already closed: %w", remoteio.ErrClosed)
	}
	return f.client, nil
}
