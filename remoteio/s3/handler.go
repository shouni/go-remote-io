package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/shouni/go-remote-io/remoteio"
)

// Scheme は、このパッケージが担当するスキーム名です（区切りは含みません）。
const Scheme = remoteio.SchemeS3

// Handler は Amazon S3 を扱う remoteio.Handler です。
//
// サーバーサイドコピー (remoteio.Copier) と署名付き URL (remoteio.Signer) の
// 任意インターフェースも実装します。
type Handler struct {
	client *awss3.Client
	// transfer は非 Seeker のストリームとマルチパートを扱うためのものです。
	//
	// 旧 feature/s3/manager の Uploader / Downloader は非推奨で、
	// feature/s3/transfermanager が後継です（aws-sdk-go-v2 discussion #3306）。
	// 後継はまだ v0.x で API が動く可能性がありますが、このエコシステムで
	// s3:// を使っているのは 1 箇所だけなので、追随の costs より
	// 非推奨 API を抱えない方を採ります。
	transfer *transfermanager.Client
}

var (
	_ remoteio.Handler = (*Handler)(nil)
	_ remoteio.Copier  = (*Handler)(nil)
	_ remoteio.Signer  = (*Handler)(nil)
)

// NewHandler は S3 クライアントを包んだハンドラを返します。
func NewHandler(client *awss3.Client) *Handler {
	h := &Handler{client: client}
	if client != nil {
		h.transfer = transfermanager.New(client)
	}
	return h
}

// Scheme は "s3" を返します。
func (h *Handler) Scheme() string { return Scheme }

// Open は S3 オブジェクトの読み取りストリームを返します。
// オブジェクトが存在しない場合のエラーは remoteio.ErrNotExist を含みます。
func (h *Handler) Open(ctx context.Context, uri string) (io.ReadCloser, error) {
	bucket, key, err := h.parseObjectURI(uri)
	if err != nil {
		return nil, err
	}

	result, err := h.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, notExist(uri)
		}
		return nil, fmt.Errorf("S3読み込み失敗 (URI: %s): %w", uri, err)
	}
	return result.Body, nil
}

// Stat は S3 オブジェクトのメタデータを返します。
// 見つからない場合のエラーは Open と同じく remoteio.ErrNotExist を含みます。
func (h *Handler) Stat(ctx context.Context, uri string) (remoteio.ObjectInfo, error) {
	bucket, key, err := h.parseObjectURI(uri)
	if err != nil {
		return remoteio.ObjectInfo{}, err
	}

	output, err := h.client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return remoteio.ObjectInfo{}, notExist(uri)
		}
		return remoteio.ObjectInfo{}, fmt.Errorf("S3属性取得失敗 (URI: %s): %w", uri, err)
	}

	return remoteio.ObjectInfo{
		URI:         uri,
		Size:        aws.ToInt64(output.ContentLength),
		ModTime:     aws.ToTime(output.LastModified),
		ContentType: aws.ToString(output.ContentType),
		Metadata:    output.Metadata,
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
		input := &awss3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
			Prefix: aws.String(prefix),
		}
		if opts.Delimiter != "" {
			input.Delimiter = aws.String(opts.Delimiter)
		}

		paginator := awss3.NewListObjectsV2Paginator(h.client, input)
		for paginator.HasMorePages() {
			output, err := paginator.NextPage(ctx)
			if err != nil {
				yield(remoteio.Entry{}, fmt.Errorf("S3リスト取得失敗 (ページネーション中, URI: %s): %w", uri, err))
				return
			}

			// 区切り文字を指定したとき、疑似ディレクトリは Contents ではなく
			// CommonPrefixes に入ります。
			for _, cp := range output.CommonPrefixes {
				key := aws.ToString(cp.Prefix)
				if key == "" || key == prefix {
					continue
				}
				entry := remoteio.Entry{
					URI:      remoteio.BuildURI(Scheme, bucket, key),
					Name:     strings.TrimPrefix(key, prefix),
					IsPrefix: true,
				}
				if !yield(entry, nil) {
					return
				}
			}

			for _, obj := range output.Contents {
				key := aws.ToString(obj.Key)
				if key == "" || key == prefix {
					continue
				}
				entry := remoteio.Entry{
					URI:     remoteio.BuildURI(Scheme, bucket, key),
					Name:    strings.TrimPrefix(key, prefix),
					Size:    aws.ToInt64(obj.Size),
					ModTime: aws.ToTime(obj.LastModified),
				}
				if !yield(entry, nil) {
					return
				}
			}
		}
	}
}

// Write は S3 オブジェクトへ書き込みます。
//
// PutObject ではなく transfermanager を通します。PutObject はボディが
// io.Seeker でないと署名前のチェックサム計算に失敗し、TLS でないエンドポイント
// （MinIO や R2、テストのフェイク）では書き込みが一切できませんでした。
// transfermanager は非 Seeker のストリームを扱え、大きなオブジェクトを
// マルチパートに分割し、途中で失敗したときはアップロードを abort するため、
// 「成功しなければ書き込み先が変化しない」という Handler の契約も満たします。
func (h *Handler) Write(ctx context.Context, uri string, src io.Reader, opts remoteio.WriteOptions) error {
	bucket, key, err := h.parseObjectURI(uri)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "S3書き込み処理開始",
		slog.String("uri", uri),
		slog.String("content_type", opts.ContentType),
		slog.String("disposition", opts.ContentDisposition),
		slog.String("cache_control", opts.CacheControl),
	)

	input := &transfermanager.UploadObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        src,
		ContentType: aws.String(opts.ContentType),
	}
	if opts.ContentDisposition != "" {
		input.ContentDisposition = aws.String(opts.ContentDisposition)
	}
	if opts.CacheControl != "" {
		input.CacheControl = aws.String(opts.CacheControl)
	}
	if len(opts.Metadata) > 0 {
		input.Metadata = opts.Metadata
	}
	if opts.IfNotExists {
		// 存在確認と書き込みの間に他のプロセスが割り込めないよう、判定を
		// ストレージ側の条件付きリクエストに委ねます。
		input.IfNoneMatch = aws.String("*")
	}

	if _, err := h.transfer.UploadObject(ctx, input); err != nil {
		if opts.IfNotExists && isPreconditionFailed(err) {
			return fmt.Errorf("S3オブジェクトは既に存在します (URI: %s): %w", uri, remoteio.ErrExist)
		}
		return fmt.Errorf("S3へのコンテンツ書き込み中にエラーが発生しました (URI: %s): %w", uri, err)
	}

	slog.DebugContext(ctx, "S3書き込み処理完了", slog.String("uri", uri))
	return nil
}

// Delete は S3 オブジェクトを削除します。不在はエラーにしません（S3 の DeleteObject 自体が冪等です）。
func (h *Handler) Delete(ctx context.Context, uri string) error {
	bucket, key, err := h.parseObjectURI(uri)
	if err != nil {
		return err
	}
	if _, err := h.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("S3オブジェクトの削除に失敗しました (URI: %s): %w", uri, err)
	}
	return nil
}

// CopyTo は S3 のサーバーサイドコピーでオブジェクトを複製します。
// remoteio.Copier の実装です。
func (h *Handler) CopyTo(ctx context.Context, src, dst string) error {
	srcBucket, srcKey, err := h.parseObjectURI(src)
	if err != nil {
		return err
	}
	dstBucket, dstKey, err := h.parseObjectURI(dst)
	if err != nil {
		return err
	}

	if _, err := h.client.CopyObject(ctx, &awss3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(copySource(srcBucket, srcKey)),
	}); err != nil {
		if isNotFound(err) {
			return notExist(src)
		}
		return fmt.Errorf("S3サーバーサイドコピーに失敗しました (%s -> %s): %w", src, dst, err)
	}
	return nil
}

// copySource は CopySource ヘッダーに載せる "bucket/key" を組み立てます。
//
// キーは URL エンコードが要りますが、区切りの "/" はそのまま残す必要があります。
// url.PathEscape は "/" まで %2F にしてしまうため、URL の Path として組んでから
// EscapedPath を取ります。
func copySource(bucket, key string) string {
	u := url.URL{Path: "/" + bucket + "/" + key}
	return strings.TrimPrefix(u.EscapedPath(), "/")
}

// parseBucketURI は URI を検証してバケット名とプレフィックスに分解します
// （オブジェクト名は空でも可）。一覧のようにプレフィックスが空でも意味を持つ操作で使います。
func (h *Handler) parseBucketURI(uri string) (bucket, prefix string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("S3クライアントが未初期化です (URI: %s): %w", uri, remoteio.ErrClosed)
	}
	return remoteio.ParseBucketURI(Scheme, uri)
}

// parseObjectURI は URI を検証してバケット名とオブジェクト名に分解します。
func (h *Handler) parseObjectURI(uri string) (bucket, key string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("S3クライアントが未初期化です (URI: %s): %w", uri, remoteio.ErrClosed)
	}
	return remoteio.ParseObjectURI(Scheme, uri)
}

// notExist は「見つからない」を remoteio.ErrNotExist を含む形で返します。
func notExist(uri string) error {
	return fmt.Errorf("S3オブジェクトが見つかりません (URI: %s): %w", uri, remoteio.ErrNotExist)
}

// isNotFound は、オブジェクトが存在しないことを表すエラーかどうかを判定します。
// HeadObject は NoSuchKey ではなく NotFound を返すため、両方を見ます。
func isNotFound(err error) bool {
	if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
		return true
	}
	_, ok := errors.AsType[*types.NotFound](err)
	return ok
}

// isPreconditionFailed は、If-None-Match の条件が満たされなかった応答かを判定します。
func isPreconditionFailed(err error) bool {
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		return apiErr.ErrorCode() == "PreconditionFailed"
	}
	return false
}

// errSeq は、反復を始める前に失敗したことを 1 度だけ伝える反復子を返します。
func errSeq(err error) iter.Seq2[remoteio.Entry, error] {
	return func(yield func(remoteio.Entry, error) bool) { yield(remoteio.Entry{}, err) }
}
