package remoteio

// NewSchemeRouter は、リモート用のハンドラにローカル関連のハンドラを併せて登録した
// Router を返します。
//
// ローカル（スキームなし）と file:// を必ず組にするのは、同じリーダーで開発時の
// ローカルファイルも読めるようにするためです。担当外のリモートスキームは登録されない
// ので、明確に未対応として弾かれます。各ファクトリが同じ組み立てを繰り返さないよう、
// ここに 1 つ置いています。
func NewSchemeRouter(handlers ...SchemeHandler) *Router {
	all := make([]SchemeHandler, 0, len(handlers)+2)
	all = append(all, handlers...)
	all = append(all, NewLocalHandler(), NewFileHandler())
	return NewRouter(all...)
}
