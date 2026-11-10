package remoteio

import (
	"context"
	"io"
)

// ctxReader is an io.Reader wrapper that lets ctx cancellation be noticed even in the middle of io.Copy.
//
// io.Copy into an os.File does not look at ctx, so a large file being written is
// written to the end even after cancellation. Cancellation is checked on every Read.
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
