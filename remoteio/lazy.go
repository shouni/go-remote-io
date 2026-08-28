package remoteio

import (
	"context"
	"io"
	"iter"
	"sync"
	"time"
)

// Lazy は、初回の操作までハンドラの生成を遅らせるラッパーです。
//
// クラウドのクライアントは生成時に認証情報を要求します。対応スキームを
// 構築時に宣言したいけれど、そのスキームが実際に使われるとは限らない、という場面
// （HTTP も GCS も S3 も受け付けるリーダーなど）では、素直に組み立てると
// 使いもしないクラウドの認証で起動が失敗します。
//
// 生成は 1 度だけ行われ、結果は成功・失敗ともに記憶されます。並行に呼ばれても
// 生成は 1 回です。利用側がこの「遅延生成 + キャッシュ」を各自書いていたものを
// ライブラリへ引き取ったものです。
func Lazy(scheme string, open func(context.Context) (Handler, error)) Handler {
	return &lazyHandler{scheme: scheme, open: open}
}

type lazyHandler struct {
	scheme string
	open   func(context.Context) (Handler, error)

	once    sync.Once
	handler Handler
	err     error
}

var (
	_ Handler = (*lazyHandler)(nil)
	_ Copier  = (*lazyHandler)(nil)
	_ Signer  = (*lazyHandler)(nil)
)

// Scheme は、生成を伴わずに担当スキームを返します。
// これが遅延できないからこそ、スキームだけを先に受け取っています。
func (l *lazyHandler) Scheme() string { return l.scheme }

// resolve はハンドラを一度だけ生成します。
func (l *lazyHandler) resolve(ctx context.Context) (Handler, error) {
	l.once.Do(func() {
		if l.open == nil {
			l.err = wrapf(ErrUnsupportedScheme, "ハンドラの生成関数が指定されていません (%s)", l.scheme)
			return
		}
		l.handler, l.err = l.open(ctx)
		if l.err == nil && l.handler == nil {
			l.err = wrapf(ErrUnsupportedScheme, "ハンドラが生成されませんでした (%s)", l.scheme)
		}
	})
	return l.handler, l.err
}

func (l *lazyHandler) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	h, err := l.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return h.Open(ctx, uri)
}

func (l *lazyHandler) Stat(ctx context.Context, uri string) (ObjectInfo, error) {
	h, err := l.resolve(ctx)
	if err != nil {
		return ObjectInfo{}, err
	}
	return h.Stat(ctx, uri)
}

func (l *lazyHandler) List(ctx context.Context, uri string, opts ListOptions) iter.Seq2[Entry, error] {
	h, err := l.resolve(ctx)
	if err != nil {
		return errSeq(err)
	}
	return h.List(ctx, uri, opts)
}

func (l *lazyHandler) Write(ctx context.Context, uri string, src io.Reader, opts WriteOptions) error {
	h, err := l.resolve(ctx)
	if err != nil {
		return err
	}
	return h.Write(ctx, uri, src, opts)
}

func (l *lazyHandler) Delete(ctx context.Context, uri string) error {
	h, err := l.resolve(ctx)
	if err != nil {
		return err
	}
	return h.Delete(ctx, uri)
}

// CopyTo は、生成したハンドラが Copier を実装していればそちらへ委譲します。
// 実装していなければ ErrNotSupported を返し、Router がストリーム中継へ落とします。
func (l *lazyHandler) CopyTo(ctx context.Context, src, dst string) error {
	h, err := l.resolve(ctx)
	if err != nil {
		return err
	}
	copier, ok := h.(Copier)
	if !ok {
		return wrapf(ErrNotSupported, "サーバーサイドコピー (%s)", l.scheme)
	}
	return copier.CopyTo(ctx, src, dst)
}

// SignURL は、生成したハンドラが Signer を実装していればそちらへ委譲します。
func (l *lazyHandler) SignURL(ctx context.Context, uri, method string, expires time.Duration) (string, error) {
	h, err := l.resolve(ctx)
	if err != nil {
		return "", err
	}
	signer, ok := h.(Signer)
	if !ok {
		return "", wrapf(ErrNotSupported, "署名付きURL (%s)", l.scheme)
	}
	return signer.SignURL(ctx, uri, method, expires)
}
