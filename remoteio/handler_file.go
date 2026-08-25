package remoteio

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
)

// PrefixFile は file:// スキームのプレフィックスです。
const PrefixFile = "file://"

// fileHandler は file:// URI をローカルパスへ読み替えて localHandler へ委譲します。
//
// ローカルは「スキームを持たないパス」のフォールバックとして扱っていますが、
// 設定ファイルや他のツールから渡る値は file:// を付けてくることがあり、
// そのままでは「未対応のURIスキームです」で弾かれます。読み替えを 1 箇所に置いて、
// ローカルの実装は localHandler のまま 1 つに保ちます。
type fileHandler struct {
	local SchemeHandler
}

var _ SchemeHandler = fileHandler{}

// NewFileHandler は file:// スキームを扱うハンドラを返します。
//
// file:///tmp/a.txt のようにホスト部が空の形を想定し、file:// を落とした残りを
// ローカルパスとして扱います（file://data/a.txt は相対パス data/a.txt になります）。
// パーセントエンコードは URI の規約どおりデコードするため、名前に % を含むファイルは
// %25 と書く必要があります。
func NewFileHandler() SchemeHandler { return fileHandler{local: NewLocalHandler()} }

// Scheme は "file://" を返します。
func (fileHandler) Scheme() string { return PrefixFile }

func (h fileHandler) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	path, err := toLocalPath(uri)
	if err != nil {
		return nil, err
	}
	return h.local.Open(ctx, path)
}

// List は列挙結果を file:// URI に戻してから callback へ渡します。
// GCS / S3 のハンドラが gs:// / s3:// の URI を返すのと同じで、
// 受け取ったパスをそのまま次の呼び出しへ渡せる形に揃えるためです。
func (h fileHandler) List(ctx context.Context, uri string, callback func(string) error, settings ListSettings) error {
	path, err := toLocalPath(uri)
	if err != nil {
		return err
	}
	return h.local.List(ctx, path, func(found string) error {
		return callback(toFileURI(found))
	}, settings)
}

func (h fileHandler) Exists(ctx context.Context, uri string) (bool, error) {
	path, err := toLocalPath(uri)
	if err != nil {
		return false, err
	}
	return h.local.Exists(ctx, path)
}

func (h fileHandler) Write(ctx context.Context, uri string, contentReader io.Reader, settings WriteSettings) error {
	path, err := toLocalPath(uri)
	if err != nil {
		return err
	}
	return h.local.Write(ctx, path, contentReader, settings)
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
	if !strings.HasPrefix(uri, PrefixFile) {
		return "", fmt.Errorf("file:// URI ではありません: %s", uri)
	}
	body := strings.TrimPrefix(uri, PrefixFile)
	if body == "" {
		return "", fmt.Errorf("パスが指定されていません: %s", uri)
	}
	decoded, err := url.PathUnescape(body)
	if err != nil {
		return "", fmt.Errorf("file:// URI のデコードに失敗しました (%s): %w", uri, err)
	}
	return filepath.FromSlash(decoded), nil
}

// toFileURI はローカルパスを file:// URI へ戻します。
func toFileURI(path string) string {
	return PrefixFile + filepath.ToSlash(path)
}
