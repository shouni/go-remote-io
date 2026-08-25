package remoteio

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// HandlerProvider は、IOFactory が担当スキームのハンドラを直接提供できることを示します。
//
// IOFactory の InputReader / OutputWriter は複合インターフェースを返すため、
// そこから「どのスキームを担当しているか」を取り出せません。複数のファクトリを
// 1 つの Router に集約するには、ハンドラそのものが要ります。
type HandlerProvider interface {
	SchemeHandler() (SchemeHandler, error)
}

// MultiFactory は複数の IOFactory を束ね、1 つの IOFactory として振る舞います。
// gs:// と s3:// を同時に扱いたい場合に使います。
//
// Router 自体は元からスキーム非依存なので、ハンドラを並べれば複数クラウドを
// 1 つのリーダーで扱えました。足りなかったのは IOFactory / Bundle の粒度で
// 同じことをする手段と、スキームごとに振り分ける URLSigner です。
type MultiFactory struct {
	factories []IOFactory
	router    *Router
	signer    URLSigner
}

var _ IOFactory = (*MultiFactory)(nil)

// NewMultiFactory は、渡された IOFactory を束ねた MultiFactory を返します。
//
// 各 IOFactory は HandlerProvider を実装している必要があります
// （gcs.ClientFactory と s3.ClientFactory はどちらも実装しています）。
// 同じスキームを複数渡した場合は後勝ちです。
//
// 成功したあとの各 factory のライフサイクルは MultiFactory が持ちます
// （MultiFactory.Close がまとめて閉じます）。失敗した場合はどれも閉じずに返すため、
// まだ呼び出し元が所有しています。NewBundle と同じ約束です。
func NewMultiFactory(factories ...IOFactory) (*MultiFactory, error) {
	m := &MultiFactory{}
	signers := make(map[string]URLSigner, len(factories))
	handlers := make([]SchemeHandler, 0, len(factories))

	for _, factory := range factories {
		if factory == nil {
			continue
		}
		provider, ok := factory.(HandlerProvider)
		if !ok {
			return nil, fmt.Errorf("SchemeHandler を提供しない IOFactory は束ねられません: %T", factory)
		}
		handler, err := provider.SchemeHandler()
		if err != nil {
			return nil, fmt.Errorf("SchemeHandler の取得に失敗しました (%T): %w", factory, err)
		}
		signer, err := factory.URLSigner()
		if err != nil {
			return nil, fmt.Errorf("URLSigner の取得に失敗しました (%T): %w", factory, err)
		}

		handlers = append(handlers, handler)
		signers[handler.Scheme()] = signer
		m.factories = append(m.factories, factory)
	}

	if len(m.factories) == 0 {
		return nil, fmt.Errorf("IOFactory が 1 つも指定されていません")
	}

	m.router = NewSchemeRouter(handlers...)
	m.signer = signerRouter{signers: signers}
	return m, nil
}

// InputReader は、束ねた全スキームを扱う InputReader を返します。
func (m *MultiFactory) InputReader() (InputReader, error) {
	if m.router == nil {
		return nil, fmt.Errorf("MultiFactory は既にクローズされています")
	}
	return m.router, nil
}

// OutputWriter は、束ねた全スキームを扱う OutputWriter を返します。
func (m *MultiFactory) OutputWriter() (OutputWriter, error) {
	if m.router == nil {
		return nil, fmt.Errorf("MultiFactory は既にクローズされています")
	}
	return m.router, nil
}

// URLSigner は、スキームごとに対応する署名器へ振り分ける URLSigner を返します。
func (m *MultiFactory) URLSigner() (URLSigner, error) {
	if m.signer == nil {
		return nil, fmt.Errorf("MultiFactory は既にクローズされています")
	}
	return m.signer, nil
}

// Schemes は、束ねたファクトリが担当するスキームを返します（ローカルは含みません）。
func (m *MultiFactory) Schemes() []string {
	if m.router == nil {
		return nil
	}
	return m.router.Schemes()
}

// Close は束ねた全ての IOFactory を解放します。
// 途中で失敗しても残りを閉じ、発生したエラーは errors.Join でまとめて返します。
// Close は冪等です。
func (m *MultiFactory) Close() error {
	factories := m.factories
	m.factories = nil
	m.router = nil
	m.signer = nil

	var errs []error
	for _, factory := range factories {
		if err := factory.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// signerRouter は、スキームを見て対応する URLSigner へ委譲します。
//
// 各署名器は自分のスキーム以外を明確に拒否する（gcs は gs:// 以外を受け付けない）
// 設計なので、束ねる側で振り分けないと片方しか使えません。
type signerRouter struct {
	signers map[string]URLSigner
}

func (s signerRouter) GenerateSignedURL(ctx context.Context, path string, method string, expires time.Duration) (string, error) {
	scheme := SchemePrefix(path)
	signer, ok := s.signers[scheme]
	if !ok {
		return "", fmt.Errorf("署名付きURLに対応していないスキームです: %s", path)
	}
	return signer.GenerateSignedURL(ctx, path, method, expires)
}
