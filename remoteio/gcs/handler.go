package gcs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"

	"github.com/shouni/go-remote-io/remoteio"
)

// Scheme は、このパッケージが担当するスキーム名です（区切りは含みません）。
const Scheme = remoteio.SchemeGCS

// Handler は Google Cloud Storage を扱う remoteio.Handler です。
//
// サーバーサイドコピー (remoteio.Copier) と署名付き URL (remoteio.Signer) の
// 任意インターフェースも実装します。どちらも同じクライアントで足りるため、
// v1 のように署名器だけ別の型として持ち回る必要はありません。
type Handler struct {
	client *storage.Client
}

var (
	_ remoteio.Handler = (*Handler)(nil)
	_ remoteio.Copier  = (*Handler)(nil)
	_ remoteio.Signer  = (*Handler)(nil)
)

// NewHandler は GCS クライアントを包んだハンドラを返します。
func NewHandler(client *storage.Client) *Handler {
	return &Handler{client: client}
}

// Scheme は "gs" を返します。
func (h *Handler) Scheme() string { return Scheme }

// Open は GCS オブジェクトの読み取りストリームを返します。
// オブジェクトが存在しない場合のエラーは remoteio.ErrNotExist を含みます。
func (h *Handler) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	bucket, object, err := h.parseObjectURI(uri)
	if err != nil {
		return nil, err
	}

	rc, err := h.client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, notExist(uri)
		}
		return nil, fmt.Errorf("GCS読み込み失敗 (URI: %s): %w", uri, err)
	}
	return rc, nil
}

// Stat は GCS オブジェクトのメタデータを返します。
// 見つからない場合のエラーは Open と同じく remoteio.ErrNotExist を含みます。
func (h *Handler) Stat(ctx context.Context, uri string) (remoteio.ObjectInfo, error) {
	bucket, object, err := h.parseObjectURI(uri)
	if err != nil {
		return remoteio.ObjectInfo{}, err
	}

	attrs, err := h.client.Bucket(bucket).Object(object).Attrs(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return remoteio.ObjectInfo{}, notExist(uri)
		}
		return remoteio.ObjectInfo{}, fmt.Errorf("GCS属性取得失敗 (URI: %s): %w", uri, err)
	}

	return remoteio.ObjectInfo{
		URI:         uri,
		Size:        attrs.Size,
		ModTime:     attrs.Updated,
		ContentType: attrs.ContentType,
		Metadata:    attrs.Metadata,
	}, nil
}

// List はプレフィックス配下のオブジェクトを列挙します。
func (h *Handler) List(ctx context.Context, uri string, opts remoteio.ListOptions) iter.Seq2[remoteio.Entry, error] {
	// Router は正規化済みの URI を渡しますが、ハンドラは公開されているため
	// 直接呼ばれる余地があります。冪等なのでここでも通します。
	uri = remoteio.ListPrefix(uri)

	bucket, prefix, err := h.parseBucketURI(uri)
	if err != nil {
		return errSeq(err)
	}

	return func(yield func(remoteio.Entry, error) bool) {
		query := &storage.Query{Prefix: prefix, Delimiter: opts.Delimiter}
		it := h.client.Bucket(bucket).Objects(ctx, query)
		for {
			attrs, err := it.Next()
			if errors.Is(err, iterator.Done) {
				return
			}
			if err != nil {
				yield(remoteio.Entry{}, fmt.Errorf("GCSリスト取得失敗 (イテレーション中, URI: %s): %w", uri, err))
				return
			}

			// 区切り文字を指定したとき、疑似ディレクトリは Name が空で Prefix に入ります。
			// v1 はここで両者を文字列へ潰していたため、呼び出し側が末尾の "/" を
			// 見て判定し直す必要がありました。
			entry := remoteio.Entry{
				Size:    attrs.Size,
				ModTime: attrs.Updated,
			}
			key := attrs.Name
			if key == "" {
				key = attrs.Prefix
				entry.IsPrefix = true
				entry.Size, entry.ModTime = 0, zeroTime
			}
			if key == "" || key == prefix {
				continue
			}

			entry.URI = remoteio.BuildURI(Scheme, bucket, key)
			entry.Name = strings.TrimPrefix(key, prefix)
			if !yield(entry, nil) {
				return
			}
		}
	}
}

// Write は GCS オブジェクトへ書き込みます。
//
// 途中で失敗した場合はアップロードを中断するため、切り詰められたオブジェクトは
// 残りません。storage.Writer.Close() はアップロードを「完了」させる API なので、
// 失敗経路でそのまま呼んではいけません（v1 はこれを呼んでおり、失敗した書き込みが
// 中途半端なオブジェクトとして残っていました）。中断の手順は io.Copy の
// 失敗経路のコメントを参照してください。
func (h *Handler) Write(ctx context.Context, uri string, src io.Reader, opts remoteio.WriteOptions) error {
	bucket, object, err := h.parseObjectURI(uri)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "GCS書き込み処理開始",
		slog.String("uri", uri),
		slog.String("content_type", opts.ContentType),
		slog.String("disposition", opts.ContentDisposition),
		slog.String("cache_control", opts.CacheControl),
	)

	// 失敗経路でアップロードを中断するための ctx です。成功して Close したあとに
	// cancel が走っても影響はありません。
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()

	handle := h.client.Bucket(bucket).Object(object)
	if opts.IfNotExists {
		// 存在確認と書き込みの間に他のプロセスが割り込めないよう、判定を
		// ストレージ側の前提条件に委ねます。
		handle = handle.If(storage.Conditions{DoesNotExist: true})
	}

	wc := handle.NewWriter(wctx)
	wc.ContentType = opts.ContentType
	if opts.ContentDisposition != "" {
		wc.ContentDisposition = opts.ContentDisposition
	}
	if opts.CacheControl != "" {
		wc.CacheControl = opts.CacheControl
	}
	if len(opts.Metadata) > 0 {
		wc.Metadata = opts.Metadata
	}

	// n は「Writer を開いたか」の判定に使います。io.Copy は 1 バイトも読めなければ
	// Write を呼ばず、その場合 SDK は内部の goroutine もリクエストも起こしていません。
	n, copyErr := io.Copy(wc, src)
	if copyErr != nil {
		cancel()

		if n > 0 {
			// 中断はパイプ側で行います。storage.Writer は内部の goroutine が
			// io.Pipe からボディを読んでアップロードするので、パイプにエラーを
			// 立てればリクエストは必ず失敗し、オブジェクトはできません。
			// ctx のキャンセルに頼るとトランスポート任せになり、実際 GCS フェイクは
			// キャンセル済みの ctx でもアップロードを完了させます。
			_ = wc.CloseWithError(copyErr) //nolint:staticcheck // 中断の正規手段。下のコメント参照
			// CloseWithError は goroutine の終了を待たないため、続けて Close します。
			// パイプには既にエラーが立っているのでアップロードが完了することはなく、
			// Close の中の <-donec で後始末まで待てます。これが無いと Write が
			// 走行中の goroutine を残したまま返ります。
			_ = wc.Close()
		}
		// n == 0 のときは Writer が未オープンです。ここで Close すると
		// openWriter が走ってゼロバイトのオブジェクトができるため、触りません。

		return fmt.Errorf("GCSへのコンテンツ書き込み中にエラーが発生しました (URI: %s): %w", uri, copyErr)
	}

	if err := wc.Close(); err != nil {
		if opts.IfNotExists && isPreconditionFailed(err) {
			return fmt.Errorf("GCSオブジェクトは既に存在します (URI: %s): %w", uri, remoteio.ErrExist)
		}
		return fmt.Errorf("GCS Writerのクローズに失敗しました (URI: %s, アップロード処理中のエラー): %w", uri, err)
	}

	slog.DebugContext(ctx, "GCS書き込み処理完了", slog.String("uri", uri))
	return nil
}

// Delete は GCS オブジェクトを削除します。不在はエラーにしません。
func (h *Handler) Delete(ctx context.Context, uri string) error {
	bucket, object, err := h.parseObjectURI(uri)
	if err != nil {
		return err
	}
	err = h.client.Bucket(bucket).Object(object).Delete(ctx)
	if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return fmt.Errorf("GCSオブジェクトの削除に失敗しました (URI: %s): %w", uri, err)
	}
	return nil
}

// CopyTo は GCS のサーバーサイドコピーでオブジェクトを複製します。
//
// remoteio.Copier の実装です。中身がクライアントを往復しないため、
// バケット間・同一バケット内のどちらでも転送量と時間を使いません。
func (h *Handler) CopyTo(ctx context.Context, src, dst string) error {
	srcBucket, srcObject, err := h.parseObjectURI(src)
	if err != nil {
		return err
	}
	dstBucket, dstObject, err := h.parseObjectURI(dst)
	if err != nil {
		return err
	}

	srcHandle := h.client.Bucket(srcBucket).Object(srcObject)
	if _, err := h.client.Bucket(dstBucket).Object(dstObject).CopierFrom(srcHandle).Run(ctx); err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return notExist(src)
		}
		return fmt.Errorf("GCSサーバーサイドコピーに失敗しました (%s -> %s): %w", src, dst, err)
	}
	return nil
}

// parseBucketURI は URI を検証してバケット名とプレフィックスに分解します
// （オブジェクト名は空でも可）。一覧のようにプレフィックスが空でも意味を持つ操作で使います。
func (h *Handler) parseBucketURI(uri string) (bucket, prefix string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("GCSクライアントが未初期化です (URI: %s): %w", uri, remoteio.ErrClosed)
	}
	return remoteio.ParseBucketURI(Scheme, uri)
}

// parseObjectURI は URI を検証してバケット名とオブジェクト名に分解します。
// オブジェクト名が空の URI (gs://bucket) を拒否する理由は remoteio.ParseObjectURI を参照してください。
func (h *Handler) parseObjectURI(uri string) (bucket, object string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("GCSクライアントが未初期化です (URI: %s): %w", uri, remoteio.ErrClosed)
	}
	return remoteio.ParseObjectURI(Scheme, uri)
}

// notExist は「見つからない」を remoteio.ErrNotExist を含む形で返します。
// 呼び出し側がスキームに依らず errors.Is で判定できることが、この抽象の前提です。
func notExist(uri string) error {
	return fmt.Errorf("GCSオブジェクトが見つかりません (URI: %s): %w", uri, remoteio.ErrNotExist)
}

// isPreconditionFailed は、DoesNotExist の前提条件が満たされなかった応答かを判定します。
func isPreconditionFailed(err error) bool {
	if apiErr, ok := errors.AsType[*googleapi.Error](err); ok {
		return apiErr.Code == 412
	}
	return false
}

// errSeq は、反復を始める前に失敗したことを 1 度だけ伝える反復子を返します。
func errSeq(err error) iter.Seq2[remoteio.Entry, error] {
	return func(yield func(remoteio.Entry, error) bool) { yield(remoteio.Entry{}, err) }
}
