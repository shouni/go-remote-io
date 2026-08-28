package remoteio

import (
	"context"
	"fmt"
	"io"
	"iter"
	"net/url"
	"path/filepath"
	"strings"
)

// filePrefix は file:// スキームのプレフィックスです。
// スキーム名から導出するため、リテラルはこの 1 箇所だけです。
const filePrefix = SchemeFile + schemeSeparator

// fileHandler は file:// URI をローカルパスへ読み替えて localHandler へ委譲します。
//
// ローカルは「スキームを持たないパス」のフォールバックとして扱っていますが、
// 設定ファイルや他のツールから渡る値は file:// を付けてくることがあり、
// そのままでは未対応スキームとして弾かれます。読み替えを 1 箇所に置いて、
// ローカルの実装は localHandler のまま 1 つに保ちます。
type fileHandler struct {
	local Handler
}

var _ Handler = fileHandler{}

// NewFileHandler は file:// スキームを扱うハンドラを返します。
//
// file:///tmp/a.txt のようにホスト部が空の形を想定し、file:// を落とした残りを
// ローカルパスとして扱います（file://data/a.txt は相対パス data/a.txt になります）。
// パーセントエンコードは URI の規約どおりデコードするため、名前に % を含むファイルは
// %25 と書く必要があります。
func NewFileHandler() Handler { return fileHandler{local: NewLocalHandler()} }

// Scheme は "file" を返します。
func (fileHandler) Scheme() string { return SchemeFile }

func (h fileHandler) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	path, err := toLocalPath(uri)
	if err != nil {
		return nil, err
	}
	return h.local.Open(ctx, path)
}

// Stat は file:// URI のメタデータを返します。URI は問い合わせに使った形へ戻します。
func (h fileHandler) Stat(ctx context.Context, uri string) (ObjectInfo, error) {
	path, err := toLocalPath(uri)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := h.local.Stat(ctx, path)
	if err != nil {
		return ObjectInfo{}, err
	}
	info.URI = uri
	return info, nil
}

// List は列挙結果の URI を file:// の形へ戻してから返します。
// GCS / S3 のハンドラが gs:// / s3:// の URI を返すのと同じで、
// 受け取った Entry をそのまま次の呼び出しへ渡せる形に揃えるためです。
func (h fileHandler) List(ctx context.Context, uri string, opts ListOptions) iter.Seq2[Entry, error] {
	path, err := toLocalPath(uri)
	if err != nil {
		return errSeq(err)
	}
	return func(yield func(Entry, error) bool) {
		for entry, err := range h.local.List(ctx, path, opts) {
			if err != nil {
				yield(Entry{}, err)
				return
			}
			entry.URI = toFileURI(entry.URI)
			if !yield(entry, nil) {
				return
			}
		}
	}
}

func (h fileHandler) Write(ctx context.Context, uri string, src io.Reader, opts WriteOptions) error {
	path, err := toLocalPath(uri)
	if err != nil {
		return err
	}
	return h.local.Write(ctx, path, src, opts)
}

func (h fileHandler) Delete(ctx context.Context, uri string) error {
	path, err := toLocalPath(uri)
	if err != nil {
		return err
	}
	return h.local.Delete(ctx, path)
}

// toLocalPath は file:// URI をローカルパスへ変換します。
func toLocalPath(uri string) (string, error) {
	if !strings.HasPrefix(uri, filePrefix) {
		return "", fmt.Errorf("%w: file:// URI ではありません (%s)", ErrInvalidURI, uri)
	}
	body := strings.TrimPrefix(uri, filePrefix)
	if body == "" {
		return "", fmt.Errorf("%w: パスが指定されていません (%s)", ErrInvalidURI, uri)
	}
	decoded, err := url.PathUnescape(body)
	if err != nil {
		return "", wrapf(err, "file:// URI のデコードに失敗しました (%s)", uri)
	}
	return filepath.FromSlash(decoded), nil
}

// toFileURI はローカルパスを file:// URI へ戻します。
func toFileURI(path string) string {
	return filePrefix + filepath.ToSlash(path)
}
