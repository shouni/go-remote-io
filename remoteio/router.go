package remoteio

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// schemeSeparator は URI スキームの区切りです。
const schemeSeparator = "://"

// Router は、URI のスキームを見て対応する SchemeHandler へ処理を委譲します。
// InputReader と OutputWriter の両方を満たします。
//
// 振り分けが実行時なのは、パスが設定やユーザー入力から来る以上、どのスキームを
// 扱うかを静的に決められないためです。一方で「どのスキームに対応しているか」は
// 構築時に決まる情報なので、ハンドラの集合として明示的に持ちます。
type Router struct {
	handlers map[string]SchemeHandler
	fallback SchemeHandler
}

var (
	_ InputReader  = (*Router)(nil)
	_ OutputWriter = (*Router)(nil)
)

// NewRouter は、渡されたハンドラを担当スキームごとに登録した Router を返します。
// Scheme() が空文字のハンドラは、どのスキームにも一致しなかったときの
// フォールバック（通常はローカルファイルシステム）になります。
// 同じスキームを複数渡した場合は後勝ちです。
func NewRouter(handlers ...SchemeHandler) *Router {
	r := &Router{handlers: make(map[string]SchemeHandler, len(handlers))}
	for _, h := range handlers {
		if h == nil {
			continue
		}
		if scheme := h.Scheme(); scheme != "" {
			r.handlers[scheme] = h
			continue
		}
		r.fallback = h
	}
	return r
}

// Schemes は登録済みのスキームを返します（フォールバックは含みません）。
func (r *Router) Schemes() []string {
	schemes := make([]string, 0, len(r.handlers))
	for scheme := range r.handlers {
		schemes = append(schemes, scheme)
	}
	return schemes
}

// resolve は path を担当するハンドラを返します。
//
// 未対応であることの判定はここ 1 箇所だけです。以前は操作ごとに
// 「クライアントが未初期化です」の nil ガードが散らばっていました。
func (r *Router) resolve(path string) (SchemeHandler, error) {
	scheme := SchemePrefix(path)
	if scheme == "" {
		if r.fallback == nil {
			return nil, fmt.Errorf("ローカルパスを扱うハンドラが登録されていません: %s", path)
		}
		return r.fallback, nil
	}
	handler, ok := r.handlers[scheme]
	if !ok {
		return nil, fmt.Errorf("未対応のURIスキームです (%s): %s", strings.TrimSuffix(scheme, schemeSeparator), path)
	}
	return handler, nil
}

// SchemePrefix は URI からスキームのプレフィックス（"gs://" など）を取り出します。
// スキームを持たない文字列（ローカルパス）では空文字を返します。
//
// 公開しているのは、呼び出し側でもスキームごとに処理を分けたい場合があるためです
// （対応スキームを自前で持つリーダーなど）。判定を各自で書くと、この関数と
// Router.resolve の間で「どこからがスキームか」の解釈がずれます。
func SchemePrefix(path string) string {
	i := strings.Index(path, schemeSeparator)
	if i <= 0 {
		return ""
	}
	return path[:i+len(schemeSeparator)]
}

// Open は指定されたパスに対応する読み取りストリームを返します。
func (r *Router) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	return dispatch(r, path, func(h SchemeHandler) (io.ReadCloser, error) {
		return h.Open(ctx, path)
	})
}

// Exists は指定されたパスにリソースが存在するかを確認します。
func (r *Router) Exists(ctx context.Context, path string) (bool, error) {
	return dispatch(r, path, func(h SchemeHandler) (bool, error) {
		return h.Exists(ctx, path)
	})
}

// List は指定されたパス配下のリソースを列挙し、コールバックへ渡します。
func (r *Router) List(ctx context.Context, path string, callback func(string) error, opts ...ListOption) error {
	handler, err := r.resolve(path)
	if err != nil {
		return err
	}
	return handler.List(ctx, path, callback, NewListSettings(opts...))
}

// Write は指定されたパスへデータを書き込みます。
func (r *Router) Write(ctx context.Context, path string, contentReader io.Reader, opts ...WriteOption) error {
	handler, err := r.resolve(path)
	if err != nil {
		return err
	}
	return handler.Write(ctx, path, contentReader, NewWriteSettings(opts...))
}

// Delete は指定されたパスのリソースを削除します。
func (r *Router) Delete(ctx context.Context, path string) error {
	handler, err := r.resolve(path)
	if err != nil {
		return err
	}
	return handler.Delete(ctx, path)
}

// dispatch は、ハンドラを解決して値を返す操作へ委譲します。
// 解決に失敗したときの戻り値をゼロ値で揃えるためだけの補助です。
func dispatch[T any](r *Router, path string, fn func(SchemeHandler) (T, error)) (T, error) {
	handler, err := r.resolve(path)
	if err != nil {
		var zero T
		return zero, err
	}
	return fn(handler)
}
