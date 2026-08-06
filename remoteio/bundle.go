package remoteio

import "fmt"

// Bundle は、1つの IOFactory から取り出した各コンポーネントをまとめて保持します。
//
// IOFactory を受け取ったアプリケーションは、ほぼ必ず InputReader / OutputWriter /
// URLSigner を個別に取り出して構造体へ詰め直します。その定型処理を1箇所に集めたものです。
// アクセサはいずれも生成済みのクライアントを包むだけで、接続も I/O も伴わないため、
// 使わないコンポーネントが含まれてもコストにはなりません。
type Bundle struct {
	Factory IOFactory
	Reader  InputReader
	Writer  OutputWriter
	Signer  URLSigner
}

// NewBundle は、factory から各コンポーネントを取り出した Bundle を返します。
//
// 成功したあとの factory のライフサイクルは Bundle が持ちます（Bundle.Close が閉じます）。
// 失敗した場合は factory を閉じずに返すため、まだ呼び出し元が所有しています。
// 組み立て途中の後始末は、他の資源とまとめて呼び出し元が行えるようにするためです。
func NewBundle(factory IOFactory) (*Bundle, error) {
	if factory == nil {
		return nil, fmt.Errorf("IOFactory が指定されていません")
	}

	reader, err := factory.InputReader()
	if err != nil {
		return nil, fmt.Errorf("InputReader の生成に失敗しました: %w", err)
	}
	writer, err := factory.OutputWriter()
	if err != nil {
		return nil, fmt.Errorf("OutputWriter の生成に失敗しました: %w", err)
	}
	signer, err := factory.URLSigner()
	if err != nil {
		return nil, fmt.Errorf("URLSigner の生成に失敗しました: %w", err)
	}

	return &Bundle{
		Factory: factory,
		Reader:  reader,
		Writer:  writer,
		Signer:  signer,
	}, nil
}

// Close は、Bundle が保持する IOFactory を解放します。
//
// nil レシーバーと nil の Factory を許容するのは、Bundle が []io.Closer へ入れて
// まとめて解放される使われ方をするためです。役割によって組み立てない資源がある構成では、
// nil のまま入ってしまう余地を残さない方が安全です。
func (b *Bundle) Close() error {
	if b == nil || b.Factory == nil {
		return nil
	}
	return b.Factory.Close()
}
