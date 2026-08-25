package remoteio

import (
	"context"
	"io"
)

// SchemeHandler は 1 つの URI スキームに対する読み書きの実装です。
//
// 以前は GCS と S3 のクライアントを 1 つの型が両方保持し、注入されていない方は nil、
// という形で「対応していないスキーム」を表現していました。その形だと未対応であることが
// 呼び出し時のエラー文字列でしか分からず、同じ nil ガードが操作ごとに散らばります。
// 対応スキームをハンドラの集合という明示的なデータにすることで、未対応の判定が
// Router.resolve の 1 箇所に集まります。
type SchemeHandler interface {
	// Scheme は、このハンドラが担当する URI プレフィックス（"gs://" など）を返します。
	// 空文字を返すハンドラは、どのプレフィックスにも一致しなかったときのフォールバック
	// （ローカルファイルシステム）として扱われます。
	Scheme() string

	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Stat(ctx context.Context, path string) (ObjectInfo, error)
	List(ctx context.Context, path string, callback func(string) error, settings ListSettings) error
	Exists(ctx context.Context, path string) (bool, error)
	Write(ctx context.Context, path string, contentReader io.Reader, settings WriteSettings) error
	Delete(ctx context.Context, path string) error
}
