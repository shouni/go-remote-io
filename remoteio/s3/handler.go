package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/shouni/go-remote-io/remoteio"
)

// Scheme は、このパッケージが担当する URI プレフィックスです。
const Scheme = remoteio.PrefixS3

// Handler は Amazon S3 を扱う remoteio.SchemeHandler です。
type Handler struct {
	client *awss3.Client
}

var _ remoteio.SchemeHandler = (*Handler)(nil)

// NewHandler は S3 クライアントを包んだハンドラを返します。
func NewHandler(client *awss3.Client) *Handler {
	return &Handler{client: client}
}

// Scheme は "s3://" を返します。
func (h *Handler) Scheme() string { return Scheme }

// Open は S3 オブジェクトの読み取りストリームを返します。
// オブジェクトが存在しない場合のエラーは os.ErrNotExist を含みます。
func (h *Handler) Open(ctx context.Context, s3URI string) (io.ReadCloser, error) {
	bucketName, objectPath, err := h.parseObjectURI(s3URI)
	if err != nil {
		return nil, err
	}

	result, err := h.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectPath),
	})
	if err != nil {
		if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
			return nil, fmt.Errorf("S3オブジェクトが見つかりません (URI: %s): %w", s3URI, os.ErrNotExist)
		}
		return nil, fmt.Errorf("S3読み込み失敗 (URI: %s): %w", s3URI, err)
	}
	return result.Body, nil
}

// List は prefix 配下のオブジェクトを列挙します。
func (h *Handler) List(ctx context.Context, s3URI string, callback func(string) error, settings remoteio.ListSettings) error {
	bucketName, prefix, err := h.parseBucketURI(s3URI)
	if err != nil {
		return err
	}
	prefix = remoteio.ListPrefix(prefix, settings)

	input := &awss3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(prefix),
	}
	if settings.Delimiter != "" {
		input.Delimiter = aws.String(settings.Delimiter)
	}
	paginator := awss3.NewListObjectsV2Paginator(h.client, input)

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("S3リスト取得失敗 (ページネーション中, URI: %s): %w", s3URI, err)
		}
		// 区切り文字を指定したとき、疑似ディレクトリは Contents ではなく CommonPrefixes に入ります。
		for _, cp := range output.CommonPrefixes {
			if cp.Prefix == nil || *cp.Prefix == prefix {
				continue
			}
			if err := callback(remoteio.BuildS3URI(bucketName, *cp.Prefix)); err != nil {
				return err
			}
		}
		for _, obj := range output.Contents {
			if obj.Key == nil || *obj.Key == prefix {
				continue
			}
			if err := callback(remoteio.BuildS3URI(bucketName, *obj.Key)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Exists は S3 オブジェクトの存在を確認します。不在は (false, nil) を返します。
func (h *Handler) Exists(ctx context.Context, s3URI string) (bool, error) {
	bucketName, objectPath, err := h.parseObjectURI(s3URI)
	if err != nil {
		return false, err
	}

	_, err = h.client.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectPath),
	})
	if err == nil {
		return true, nil
	}

	// HeadObject は NoSuchKey ではなく NotFound を返すことがあるため両方見ます。
	if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
		return false, nil
	}
	if _, ok := errors.AsType[*types.NotFound](err); ok {
		return false, nil
	}

	return false, fmt.Errorf("S3属性取得失敗: %w", err)
}

// Write は S3 オブジェクトへ書き込みます。
func (h *Handler) Write(ctx context.Context, s3URI string, contentReader io.Reader, settings remoteio.WriteSettings) error {
	bucketName, objectPath, err := h.parseObjectURI(s3URI)
	if err != nil {
		return fmt.Errorf("S3への書き込みに失敗しました: %w", err)
	}

	slog.DebugContext(ctx, "S3書き込み処理開始",
		slog.String("uri", s3URI),
		slog.String("content_type", settings.ContentType),
		slog.String("disposition", settings.ContentDisposition),
		slog.String("cache_control", settings.CacheControl),
	)

	input := &awss3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(objectPath),
		Body:        contentReader,
		ContentType: aws.String(settings.ContentType),
	}
	if settings.ContentDisposition != "" {
		input.ContentDisposition = aws.String(settings.ContentDisposition)
	}
	if settings.CacheControl != "" {
		input.CacheControl = aws.String(settings.CacheControl)
	}

	if _, err := h.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("S3へのコンテンツ書き込み中にエラーが発生しました (URI: %s): %w", s3URI, err)
	}

	slog.DebugContext(ctx, "S3書き込み処理完了", slog.String("uri", s3URI))
	return nil
}

// Delete は S3 オブジェクトを削除します。
func (h *Handler) Delete(ctx context.Context, s3URI string) error {
	bucketName, objectPath, err := h.parseObjectURI(s3URI)
	if err != nil {
		return err
	}
	if _, err := h.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectPath),
	}); err != nil {
		return fmt.Errorf("S3オブジェクトの削除に失敗しました: %w", err)
	}
	return nil
}

// parseBucketURI は URI を検証してバケット名とプレフィックスに分解します（オブジェクト名は空でも可）。
// 一覧のように prefix が空でも意味を持つ操作で使います。
func (h *Handler) parseBucketURI(s3URI string) (bucket, prefix string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("S3クライアントが未初期化です (URI: %s)", s3URI)
	}
	return remoteio.ParseSchemeURI(Scheme, s3URI)
}

// parseObjectURI は URI を検証してバケット名とオブジェクト名に分解します。
// オブジェクト名が空の URI (s3://bucket) を拒否する理由は ParseSchemeObjectURI を参照してください。
func (h *Handler) parseObjectURI(s3URI string) (bucket, object string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("S3クライアントが未初期化です (URI: %s)", s3URI)
	}
	return remoteio.ParseSchemeObjectURI(Scheme, s3URI)
}
