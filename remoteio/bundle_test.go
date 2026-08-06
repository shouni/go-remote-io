package remoteio_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shouni/go-remote-io/remoteio"
)

// stubFactory は、どのアクセサで失敗するかを指定できる IOFactory です。
// 実 I/O を伴わない範囲で NewBundle の振り分けと後始末を確認するために使います。
type stubFactory struct {
	readerErr error
	writerErr error
	signerErr error
	closeErr  error
	closed    int
}

var _ remoteio.IOFactory = (*stubFactory)(nil)

func (f *stubFactory) InputReader() (remoteio.InputReader, error) {
	if f.readerErr != nil {
		return nil, f.readerErr
	}
	return remoteio.NewRouter(remoteio.NewLocalHandler()), nil
}

func (f *stubFactory) OutputWriter() (remoteio.OutputWriter, error) {
	if f.writerErr != nil {
		return nil, f.writerErr
	}
	return remoteio.NewRouter(remoteio.NewLocalHandler()), nil
}

func (f *stubFactory) URLSigner() (remoteio.URLSigner, error) {
	if f.signerErr != nil {
		return nil, f.signerErr
	}
	return stubSigner{}, nil
}

// stubSigner は URLSigner を満たすだけのスタブです。
type stubSigner struct{}

func (stubSigner) GenerateSignedURL(context.Context, string, string, time.Duration) (string, error) {
	return "", nil
}

func (f *stubFactory) Close() error {
	f.closed++
	return f.closeErr
}

func TestNewBundle(t *testing.T) {
	factory := &stubFactory{}

	bundle, err := remoteio.NewBundle(factory)

	require.NoError(t, err)
	require.NotNil(t, bundle)
	assert.Same(t, factory, bundle.Factory)
	assert.NotNil(t, bundle.Reader)
	assert.NotNil(t, bundle.Writer)
	assert.NotNil(t, bundle.Signer)
}

func TestNewBundleRejectsNilFactory(t *testing.T) {
	bundle, err := remoteio.NewBundle(nil)

	require.Error(t, err)
	assert.Nil(t, bundle)
}

// アクセサが失敗したとき、どれが失敗したか分かる形で返り、
// factory は閉じられずに呼び出し元の手に残ること。
// 組み立て途中の後始末は、他の資源とまとめて呼び出し元が行うためです。
func TestNewBundleWrapsAccessorErrorWithoutClosingFactory(t *testing.T) {
	sentinel := errors.New("アクセサの失敗")

	tests := []struct {
		name    string
		factory *stubFactory
		wantIn  string
	}{
		{name: "InputReader", factory: &stubFactory{readerErr: sentinel}, wantIn: "InputReader"},
		{name: "OutputWriter", factory: &stubFactory{writerErr: sentinel}, wantIn: "OutputWriter"},
		{name: "URLSigner", factory: &stubFactory{signerErr: sentinel}, wantIn: "URLSigner"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle, err := remoteio.NewBundle(tt.factory)

			require.Error(t, err)
			assert.Nil(t, bundle)
			assert.ErrorIs(t, err, sentinel)
			assert.Contains(t, err.Error(), tt.wantIn)
			assert.Zero(t, tt.factory.closed, "組み立て失敗時に factory を閉じてはならない（所有権は呼び出し元に残る）")
		})
	}
}

func TestBundleCloseClosesFactory(t *testing.T) {
	factory := &stubFactory{}
	bundle, err := remoteio.NewBundle(factory)
	require.NoError(t, err)

	require.NoError(t, bundle.Close())
	assert.Equal(t, 1, factory.closed)
}

func TestBundleClosePropagatesFactoryError(t *testing.T) {
	sentinel := errors.New("クローズの失敗")
	bundle, err := remoteio.NewBundle(&stubFactory{closeErr: sentinel})
	require.NoError(t, err)

	assert.ErrorIs(t, bundle.Close(), sentinel)
}

// []io.Closer へまとめて入れる使われ方に備え、nil でも落ちないこと。
func TestBundleCloseToleratesNil(t *testing.T) {
	var nilBundle *remoteio.Bundle
	assert.NoError(t, nilBundle.Close())

	assert.NoError(t, (&remoteio.Bundle{}).Close())
}
