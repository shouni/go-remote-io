// Package memio は、インメモリの remoteio.Handler を提供します。
//
// テストでリモートストレージの代わりに使うためのものです。docker も認証情報も
// ネットワークも要らず、GCS / S3 のハンドラと同じ契約を満たします
// （storetest.TestHandler を通しているのが根拠です）。
//
// これが無かった頃は、利用側の各リポジトリが Open / List / Write のフェイクを
// 手書きしていました。エコシステム全体で 21 のテストファイルに 72 のフェイクメソッドが
// あり、区切り文字による疑似ディレクトリの畳み込みをそれぞれ再実装していました。
// 本物とずれても CI は緑のままになる、という状態です。
package memio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shouni/go-remote-io/remoteio"
)

// DefaultScheme は、スキームを指定しなかった場合に担当するスキーム名です。
const DefaultScheme = "mem"

// Handler はインメモリの remoteio.Handler です。
// サーバーサイドコピー (remoteio.Copier) も実装します。
//
// 並行に使っても安全です。実装がリモートの代わりである以上、
// 呼び出し側が並行に叩くテストを書けなければ意味がありません。
type Handler struct {
	scheme string

	mu      sync.RWMutex
	objects map[string]object

	// failOn は、操作を意図的に失敗させるためのフックです。
	// 「書き込みが途中で失敗したときに呼び出し側がどう振る舞うか」のような、
	// 正常系では通らない経路を試すために置いています。
	failOn func(op, uri string) error
	// now は時刻の供給元です。テストが更新時刻を固定できるようにしています。
	now func() time.Time
}

// object は保存された 1 件です。
type object struct {
	data               []byte
	contentType        string
	cacheControl       string
	contentDisposition string
	metadata           map[string]string
	modTime            time.Time
}

var (
	_ remoteio.Handler = (*Handler)(nil)
	_ remoteio.Copier  = (*Handler)(nil)
)

// Option は Handler の生成方法を変える Functional Option です。
type Option func(*Handler)

// WithScheme は担当するスキーム名を指定します（既定は DefaultScheme）。
//
// gs や s3 を渡せば、そのスキームのハンドラとして振る舞います。
// 利用側のテストが本番と同じ URI を書けるようにするためのものです。
func WithScheme(scheme string) Option {
	return func(h *Handler) { h.scheme = scheme }
}

// WithFailure は、操作を意図的に失敗させるフックを登録します。
// op は "open" / "stat" / "list" / "write" / "delete" / "copy" のいずれかです。
// nil を返せば通常どおり処理します。
func WithFailure(fail func(op, uri string) error) Option {
	return func(h *Handler) { h.failOn = fail }
}

// WithClock は時刻の供給元を差し替えます。更新時刻を検証するテスト向けです。
func WithClock(now func() time.Time) Option {
	return func(h *Handler) { h.now = now }
}

// New はインメモリのハンドラを返します。
func New(opts ...Option) *Handler {
	h := &Handler{
		scheme:  DefaultScheme,
		objects: make(map[string]object),
		now:     time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

// Scheme は担当するスキーム名を返します。
func (h *Handler) Scheme() string { return h.scheme }

// Seed は、Write を通さずに内容を流し込みます。
// テストの前提を組み立てるためのもので、WithFailure の影響を受けません。
//
// メタデータまで指定したい場合は Write を使ってください。前提の組み立てで
// 要るのはほぼ内容だけなので、ここは引数を増やしていません。
func (h *Handler) Seed(uri string, data []byte) error {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.objects[path(bucket, key)] = h.newObject(data, remoteio.WriteOptions{
		ContentType: remoteio.DefaultContentType,
	})
	return nil
}

// URIs は保存されている全オブジェクトの URI を辞書順で返します。
// 「どこへ書かれたか」をテストで確かめるためのものです。
func (h *Handler) URIs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]string, 0, len(h.objects))
	for key := range h.objects {
		bucket, object, _ := strings.Cut(key, "/")
		out = append(out, remoteio.BuildURI(h.scheme, bucket, object))
	}
	slices.Sort(out)
	return out
}

// Len は保存されているオブジェクト数を返します。
func (h *Handler) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.objects)
}

// Open は保存された内容の読み取りストリームを返します。
func (h *Handler) Open(_ context.Context, uri string) (io.ReadCloser, error) {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return nil, err
	}
	if err := h.fail("open", uri); err != nil {
		return nil, err
	}

	h.mu.RLock()
	obj, ok := h.objects[path(bucket, key)]
	h.mu.RUnlock()
	if !ok {
		return nil, notExist(uri)
	}
	// 保存している側のスライスを渡すと、読み手が書き換えられます。
	return io.NopCloser(bytes.NewReader(slices.Clone(obj.data))), nil
}

// Stat はメタデータを返します。
func (h *Handler) Stat(_ context.Context, uri string) (remoteio.ObjectInfo, error) {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return remoteio.ObjectInfo{}, err
	}
	if err := h.fail("stat", uri); err != nil {
		return remoteio.ObjectInfo{}, err
	}

	h.mu.RLock()
	obj, ok := h.objects[path(bucket, key)]
	h.mu.RUnlock()
	if !ok {
		return remoteio.ObjectInfo{}, notExist(uri)
	}

	return remoteio.ObjectInfo{
		URI:         uri,
		Size:        int64(len(obj.data)),
		ModTime:     obj.modTime,
		ContentType: obj.contentType,
		Metadata:    maps.Clone(obj.metadata),
	}, nil
}

// List はプレフィックス配下を列挙します。
//
// 区切り文字の扱いは GCS / S3 に合わせています。ここが本物とずれると、
// このパッケージを使うテスト全部が意味を失うため、実装の要点です。
func (h *Handler) List(_ context.Context, uri string, opts remoteio.ListOptions) iter.Seq2[remoteio.Entry, error] {
	uri = remoteio.ListPrefix(uri)

	bucket, prefix, err := h.parseBucket(uri)
	if err != nil {
		return errSeq(err)
	}
	if err := h.fail("list", uri); err != nil {
		return errSeq(err)
	}

	h.mu.RLock()
	keys := make([]string, 0, len(h.objects))
	snapshot := make(map[string]object, len(h.objects))
	for key, obj := range h.objects {
		keys = append(keys, key)
		snapshot[key] = obj
	}
	h.mu.RUnlock()
	slices.Sort(keys)

	base := path(bucket, prefix)
	return func(yield func(remoteio.Entry, error) bool) {
		seen := make(map[string]bool)
		for _, key := range keys {
			if !strings.HasPrefix(key, base) {
				continue
			}
			rest := strings.TrimPrefix(key, base)
			if rest == "" {
				continue
			}

			if opts.Delimiter != "" {
				if i := strings.Index(rest, opts.Delimiter); i >= 0 {
					// 区切りより先は疑似ディレクトリへ畳みます。
					name := rest[:i+len(opts.Delimiter)]
					if seen[name] {
						continue
					}
					seen[name] = true
					entry := remoteio.Entry{
						URI:      remoteio.BuildURI(h.scheme, bucket, prefix+name),
						Name:     name,
						IsPrefix: true,
					}
					if !yield(entry, nil) {
						return
					}
					continue
				}
			}

			obj := snapshot[key]
			entry := remoteio.Entry{
				URI:     remoteio.BuildURI(h.scheme, bucket, prefix+rest),
				Name:    rest,
				Size:    int64(len(obj.data)),
				ModTime: obj.modTime,
			}
			if !yield(entry, nil) {
				return
			}
		}
	}
}

// Write は内容を保存します。
//
// 先に全部読み切ってから登録するため、途中で失敗しても保存済みの内容は変化しません
// （Handler が要求する「成功しなければ書き込み先が変化しない」を満たします）。
func (h *Handler) Write(_ context.Context, uri string, src io.Reader, opts remoteio.WriteOptions) error {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return err
	}
	if err := h.fail("write", uri); err != nil {
		return err
	}

	data, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("メモリへの書き込みに失敗しました (URI: %s): %w", uri, err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	full := path(bucket, key)
	if opts.IfNotExists {
		if _, ok := h.objects[full]; ok {
			return fmt.Errorf("オブジェクトは既に存在します (URI: %s): %w", uri, remoteio.ErrExist)
		}
	}
	h.objects[full] = h.newObject(data, opts)
	return nil
}

// Delete はオブジェクトを削除します。不在はエラーにしません。
func (h *Handler) Delete(_ context.Context, uri string) error {
	bucket, key, err := h.parseObject(uri)
	if err != nil {
		return err
	}
	if err := h.fail("delete", uri); err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.objects, path(bucket, key))
	return nil
}

// CopyTo は保存された内容を複製します。remoteio.Copier の実装です。
func (h *Handler) CopyTo(_ context.Context, src, dst string) error {
	srcBucket, srcKey, err := h.parseObject(src)
	if err != nil {
		return err
	}
	dstBucket, dstKey, err := h.parseObject(dst)
	if err != nil {
		return err
	}
	if err := h.fail("copy", src); err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	obj, ok := h.objects[path(srcBucket, srcKey)]
	if !ok {
		return notExist(src)
	}
	obj.data = slices.Clone(obj.data)
	obj.metadata = maps.Clone(obj.metadata)
	obj.modTime = h.now()
	h.objects[path(dstBucket, dstKey)] = obj
	return nil
}

// newObject は保存する値を組み立てます。呼び出し側が渡したマップを
// そのまま抱えると、あとから書き換えられるため複製します。
func (h *Handler) newObject(data []byte, opts remoteio.WriteOptions) object {
	return object{
		data:               slices.Clone(data),
		contentType:        opts.ContentType,
		cacheControl:       opts.CacheControl,
		contentDisposition: opts.ContentDisposition,
		metadata:           maps.Clone(opts.Metadata),
		modTime:            h.now(),
	}
}

func (h *Handler) fail(op, uri string) error {
	if h.failOn == nil {
		return nil
	}
	return h.failOn(op, uri)
}

func (h *Handler) parseBucket(uri string) (bucket, prefix string, err error) {
	return remoteio.ParseBucketURI(h.scheme, uri)
}

func (h *Handler) parseObject(uri string) (bucket, key string, err error) {
	return remoteio.ParseObjectURI(h.scheme, uri)
}

// path は保存用のキーです。バケットを跨いだ前方一致を起こさないよう、
// バケット名とオブジェクト名を "/" で繋いだ 1 本の文字列にします。
func path(bucket, key string) string { return bucket + "/" + key }

func notExist(uri string) error {
	return fmt.Errorf("オブジェクトが見つかりません (URI: %s): %w", uri, remoteio.ErrNotExist)
}

func errSeq(err error) iter.Seq2[remoteio.Entry, error] {
	return func(yield func(remoteio.Entry, error) bool) { yield(remoteio.Entry{}, err) }
}
