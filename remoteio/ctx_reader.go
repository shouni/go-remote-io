package remoteio

import (
	"context"
	"io"
)

// ctxReader は io.Copy の途中でも ctx のキャンセルを検知できるようにする io.Reader ラッパーです。
//
// os.File への io.Copy は ctx を見ないため、大きなファイルを書いている最中に
// キャンセルされても最後まで書き切ってしまいます。Read の都度キャンセルを確認します。
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (cr *ctxReader) Read(p []byte) (int, error) {
	if err := cr.ctx.Err(); err != nil {
		return 0, err
	}
	return cr.r.Read(p)
}
