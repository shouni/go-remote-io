// Package gcs provides a remoteio.Handler for Google Cloud Storage and a factory
// that manages its lifecycle.
package gcs

import (
	"context"
	"fmt"
	"sync"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"

	"github.com/shouni/go-remote-io/remoteio"
)

// ClientFactory owns the lifecycle of a GCS client.
//
// Close and each accessor are safe to call concurrently.
type ClientFactory struct {
	mu     sync.RWMutex
	client *storage.Client
	// ownsClient reports whether Close may close the client.
	// The lifecycle of a client injected with WithClient belongs to the caller.
	ownsClient bool
}

var _ remoteio.Factory = (*ClientFactory)(nil)

// settings is the result of resolving Option values.
type settings struct {
	client     *storage.Client
	clientOpts []option.ClientOption
}

// Option is a functional option that changes how a ClientFactory is created.
type Option func(*settings)

// WithClient uses an already created GCS client.
//
// The lifecycle of this client stays with the caller, and ClientFactory.Close does
// not close it (with two parties able to close it, the caller could not tell which
// one owns it).
func WithClient(client *storage.Client) Option {
	return func(s *settings) { s.client = client }
}

// WithClientOptions sets the options passed to storage.NewClient.
// Use it to point at an emulator (option.WithEndpoint) or to swap credentials.
func WithClientOptions(opts ...option.ClientOption) Option {
	return func(s *settings) { s.clientOpts = append(s.clientOpts, opts...) }
}

// New creates a ClientFactory.
// Without options, the client is created with Application Default Credentials.
func New(ctx context.Context, opts ...Option) (*ClientFactory, error) {
	var cfg settings
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	if cfg.client != nil {
		return &ClientFactory{client: cfg.client, ownsClient: false}, nil
	}

	client, err := storage.NewClient(ctx, cfg.clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("initialize GCS client: %w", err)
	}
	return &ClientFactory{client: client, ownsClient: true}, nil
}

// Close releases the GCS client it holds. It is idempotent.
// A client injected with WithClient is not closed; only the reference is dropped.
func (f *ClientFactory) Close() error {
	f.mu.Lock()
	client, owns := f.client, f.ownsClient
	f.client = nil
	f.mu.Unlock()

	if client == nil || !owns {
		return nil
	}
	return client.Close()
}

// Handler returns the handler responsible for gs://.
//
// To handle several clouds in one Store, line up the handlers taken from each
// factory in remoteio.NewStore.
func (f *ClientFactory) Handler() (remoteio.Handler, error) {
	client, err := f.gcsClient()
	if err != nil {
		return nil, err
	}
	return NewHandler(client), nil
}

// Store returns a Store that handles gs:// and local paths
// (s3:// is not registered, so it is clearly unsupported).
func (f *ClientFactory) Store() (remoteio.Store, error) {
	handler, err := f.Handler()
	if err != nil {
		return nil, err
	}
	return remoteio.NewStore(handler), nil
}

// gcsClient checks that the client is still alive and returns it.
func (f *ClientFactory) gcsClient() (*storage.Client, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.client == nil {
		return nil, fmt.Errorf("GCS client is already closed or not initialized: %w", remoteio.ErrClosed)
	}
	return f.client, nil
}
