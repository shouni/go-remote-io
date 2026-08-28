package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"slices"
	"time"
)

// Router は、URI のスキームを見て対応する Handler へ処理を委譲する Store です。
//
// 振り分けが実行時なのは、パスが設定やユーザー入力から来る以上、どのスキームを
// 扱うかを静的に決められないためです。一方で「どのスキームに対応しているか」は
// 構築時に決まる情報なので、ハンドラの集合として明示的に持ちます。
// ハンドラの集合は構築後に変わらないため、並行に使っても安全です。
type Router struct {
	handlers map[string]Handler
	fallback Handler
}

var _ Store = (*Router)(nil)

// NewRouter は、渡されたハンドラを担当スキームごとに登録した Router を返します。
// Scheme() が空文字のハンドラは、どのスキームにも一致しなかったときの
// フォールバック（通常はローカルファイルシステム）になります。
// 同じスキームを複数渡した場合は後勝ちです。
func NewRouter(handlers ...Handler) *Router {
	r := &Router{handlers: make(map[string]Handler, len(handlers))}
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

// NewStore は、リモート用のハンドラにローカル関連のハンドラを併せて登録した
// Store を返します。
//
// ローカル（スキームなし）と file:// を必ず組にするのは、同じストアで開発時の
// ローカルファイルも読めるようにするためです。担当外のリモートスキームは
// 登録されないので、明確に未対応として弾かれます。
func NewStore(handlers ...Handler) *Router {
	all := make([]Handler, 0, len(handlers)+2)
	all = append(all, handlers...)
	all = append(all, NewLocalHandler(), NewFileHandler())
	return NewRouter(all...)
}

// Schemes は登録済みのスキーム名を返します（フォールバックは含みません）。
// 呼ぶたびに順序が変わらないよう、辞書順にそろえて返します。
func (r *Router) Schemes() []string {
	return slices.Sorted(maps.Keys(r.handlers))
}

// resolve は uri を担当するハンドラを返します。
// 未対応であることの判定はここ 1 箇所だけです。
func (r *Router) resolve(uri string) (Handler, error) {
	scheme := Scheme(uri)
	if scheme == "" {
		if r.fallback == nil {
			return nil, fmt.Errorf("%w: ローカルパスを扱うハンドラが登録されていません (%s)", ErrUnsupportedScheme, uri)
		}
		return r.fallback, nil
	}
	handler, ok := r.handlers[scheme]
	if !ok {
		return nil, fmt.Errorf("%w: %s (%s)", ErrUnsupportedScheme, scheme, uri)
	}
	return handler, nil
}

// Open は指定されたパスに対応する読み取りストリームを返します。
func (r *Router) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	return dispatch(r, name, func(h Handler) (io.ReadCloser, error) {
		return h.Open(ctx, name)
	})
}

// Stat は指定されたパスのメタデータを返します。
func (r *Router) Stat(ctx context.Context, name string) (ObjectInfo, error) {
	return dispatch(r, name, func(h Handler) (ObjectInfo, error) {
		return h.Stat(ctx, name)
	})
}

// Exists は指定されたパスにオブジェクトが存在するかを返します。
//
// Handler に Exists を持たせず Stat から導出するのは、実装ごとに
// 「不在を (false, nil) で返す」約束を取り違える余地を消すためです。
func (r *Router) Exists(ctx context.Context, name string) (bool, error) {
	if _, err := r.Stat(ctx, name); err != nil {
		if errors.Is(err, ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// List は指定されたパス配下を列挙します。
func (r *Router) List(ctx context.Context, name string, opts ...ListOption) iter.Seq2[Entry, error] {
	handler, err := r.resolve(name)
	if err != nil {
		return errSeq(err)
	}
	return handler.List(ctx, ListPrefix(name), newListOptions(opts...))
}

// Write は指定されたパスへデータを書き込みます。
func (r *Router) Write(ctx context.Context, name string, src io.Reader, opts ...WriteOption) error {
	handler, err := r.resolve(name)
	if err != nil {
		return err
	}
	return handler.Write(ctx, name, src, newWriteOptions(opts...))
}

// Delete は指定されたパスのリソースを削除します。
func (r *Router) Delete(ctx context.Context, name string) error {
	handler, err := r.resolve(name)
	if err != nil {
		return err
	}
	return handler.Delete(ctx, name)
}

// Copy は src の内容を dst へ複製します。
//
// 両者が同じハンドラに解決され、そのハンドラが Copier を実装していれば
// サーバーサイドコピーへ落とします。それ以外はストリームで中継します。
func (r *Router) Copy(ctx context.Context, src, dst string, opts ...WriteOption) error {
	srcHandler, err := r.resolve(src)
	if err != nil {
		return err
	}
	dstHandler, err := r.resolve(dst)
	if err != nil {
		return err
	}

	// 同一スキームかどうかで判定します。ハンドラ値そのものの比較は、
	// 比較不可能な型を内包していると panic するため使いません。
	if srcHandler.Scheme() == dstHandler.Scheme() {
		if copier, ok := srcHandler.(Copier); ok {
			err := copier.CopyTo(ctx, src, dst)
			// ErrNotSupported だけはストリーム中継へ落とします。サーバーサイド
			// コピーの可否は対象によって変わることがあり（S3 の CopyObject は
			// 5GB を超えると使えません）、実装がそれを実行時に伝えられないと、
			// 呼び出し側が「大きいときだけ別の書き方」を持つことになります。
			if err == nil {
				return nil
			}
			if !errors.Is(err, ErrNotSupported) {
				return wrapf(err, "コピーに失敗しました (%s -> %s)", src, dst)
			}
		}
	}

	rc, err := srcHandler.Open(ctx, src)
	if err != nil {
		return wrapf(err, "コピー元のオープンに失敗しました (%s)", src)
	}
	defer func() { _ = rc.Close() }()

	if err := dstHandler.Write(ctx, dst, rc, newWriteOptions(opts...)); err != nil {
		return wrapf(err, "コピー先への書き込みに失敗しました (%s -> %s)", src, dst)
	}
	return nil
}

// SignURL は署名付き URL を生成します。
func (r *Router) SignURL(ctx context.Context, name, method string, expires time.Duration) (string, error) {
	handler, err := r.resolve(name)
	if err != nil {
		return "", err
	}
	signer, ok := handler.(Signer)
	if !ok {
		return "", fmt.Errorf("%w: 署名付きURL (%s)", ErrNotSupported, name)
	}
	return signer.SignURL(ctx, name, method, expires)
}

// Sub は、プレフィックスに固定されたストアを返します。
func (r *Router) Sub(prefix string) Store {
	return Sub(r, prefix)
}

// Sub は、任意の Store をプレフィックスに固定したストアを返します。
//
// Store を包んで振る舞いを足す型（呼び出しを記録するテストのフェイクなど）は、
// Sub メソッドをこの関数へ委譲してください。埋め込みから昇格した Sub は
// 埋め込まれた側をスコープの土台にするため、包んだ側の振る舞いが
// スコープの先で失われます。
//
//	func (d *decorator) Sub(prefix string) remoteio.Store {
//		return remoteio.Sub(d, prefix)
//	}
func Sub(store Store, prefix string) Store {
	return &scopedStore{root: store, prefix: prefix}
}

// dispatch は、ハンドラを解決して値を返す操作へ委譲します。
// 解決に失敗したときの戻り値をゼロ値で揃えるためだけの補助です。
func dispatch[T any](r *Router, uri string, fn func(Handler) (T, error)) (T, error) {
	handler, err := r.resolve(uri)
	if err != nil {
		var zero T
		return zero, err
	}
	return fn(handler)
}

// errSeq は、反復を始める前に失敗したことを 1 度だけ伝える反復子を返します。
func errSeq(err error) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) { yield(Entry{}, err) }
}

// scopedStore は、プレフィックスに固定された Store です。
//
// 呼び出しのたびにバケット名を連れ回す代わりに、組み立て時に一度だけ決めます。
// これが無いと、利用側は「バケット名 + パス」から URI を組む薄いメソッドを
// リポジトリごとに書き直すことになります。実際そうなっており、それでも足りずに
// "gs://" を直書きする箇所が利用側に多数ありました。
type scopedStore struct {
	// root は Store インターフェースです。具象の Router に固定すると、
	// Store を包んだ型が Sub を経た瞬間に素通しされます。
	root   Store
	prefix string
}

var _ Store = (*scopedStore)(nil)

// resolveName は相対名をスコープ内の完全なパスへ変換します。
//
// スキーム付きの絶対 URI を弾くのは、スコープを絞ったつもりのコードが
// 別のバケットへ書けてしまうのを防ぐためです。
func (s *scopedStore) resolveName(name string) (string, error) {
	if Scheme(name) != "" {
		return "", fmt.Errorf("%w: %s (スコープ: %s)", ErrAbsoluteName, name, s.prefix)
	}
	return Join(s.prefix, name), nil
}

func (s *scopedStore) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	full, err := s.resolveName(name)
	if err != nil {
		return nil, err
	}
	return s.root.Open(ctx, full)
}

func (s *scopedStore) Stat(ctx context.Context, name string) (ObjectInfo, error) {
	full, err := s.resolveName(name)
	if err != nil {
		return ObjectInfo{}, err
	}
	return s.root.Stat(ctx, full)
}

func (s *scopedStore) Exists(ctx context.Context, name string) (bool, error) {
	full, err := s.resolveName(name)
	if err != nil {
		return false, err
	}
	return s.root.Exists(ctx, full)
}

func (s *scopedStore) List(ctx context.Context, name string, opts ...ListOption) iter.Seq2[Entry, error] {
	full, err := s.resolveName(name)
	if err != nil {
		return errSeq(err)
	}
	return s.root.List(ctx, full, opts...)
}

func (s *scopedStore) Write(ctx context.Context, name string, src io.Reader, opts ...WriteOption) error {
	full, err := s.resolveName(name)
	if err != nil {
		return err
	}
	return s.root.Write(ctx, full, src, opts...)
}

func (s *scopedStore) Delete(ctx context.Context, name string) error {
	full, err := s.resolveName(name)
	if err != nil {
		return err
	}
	return s.root.Delete(ctx, full)
}

func (s *scopedStore) Copy(ctx context.Context, src, dst string, opts ...WriteOption) error {
	fullSrc, err := s.resolveName(src)
	if err != nil {
		return err
	}
	fullDst, err := s.resolveName(dst)
	if err != nil {
		return err
	}
	return s.root.Copy(ctx, fullSrc, fullDst, opts...)
}

func (s *scopedStore) SignURL(ctx context.Context, name, method string, expires time.Duration) (string, error) {
	full, err := s.resolveName(name)
	if err != nil {
		return "", err
	}
	return s.root.SignURL(ctx, full, method, expires)
}

// Sub はスコープをさらに絞ったストアを返します。
func (s *scopedStore) Sub(prefix string) Store {
	return Sub(s.root, Join(s.prefix, prefix))
}
