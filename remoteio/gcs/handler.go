package gcs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"

	"github.com/shouni/go-remote-io/remoteio"
)

// Scheme は、このパッケージが担当する URI プレフィックスです。
const Scheme = remoteio.PrefixGCS

// Handler は Google Cloud Storage を扱う remoteio.SchemeHandler です。
type Handler struct {
	client *storage.Client
}

var _ remoteio.SchemeHandler = (*Handler)(nil)

// NewHandler は GCS クライアントを包んだハンドラを返します。
func NewHandler(client *storage.Client) *Handler {
	return &Handler{client: client}
}

// Scheme は "gs://" を返します。
func (h *Handler) Scheme() string { return Scheme }

// Open は GCS オブジェクトの読み取りストリームを返します。
// オブジェクトが存在しない場合のエラーは os.ErrNotExist を含みます。
func (h *Handler) Open(ctx context.Context, gcsURI string) (io.ReadCloser, error) {
	bucketName, objectName, err := h.parseObjectURI(gcsURI)
	if err != nil {
		return nil, err
	}

	rc, err := h.client.Bucket(bucketName).Object(objectName).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, fmt.Errorf("GCSオブジェクトが見つかりません (URI: %s): %w", gcsURI, os.ErrNotExist)
		}
		return nil, fmt.Errorf("GCS読み込み失敗 (URI: %s): %w", gcsURI, err)
	}
	return rc, nil
}

// List は prefix 配下のオブジェクトを列挙します。
func (h *Handler) List(ctx context.Context, gcsURI string, callback func(string) error, settings remoteio.ListSettings) error {
	bucketName, prefix, err := remoteio.ParseRemoteURI(gcsURI)
	if err != nil {
		return err
	}
	prefix = remoteio.ListPrefix(prefix, settings)

	query := &storage.Query{Prefix: prefix, Delimiter: settings.Delimiter}
	it := h.client.Bucket(bucketName).Objects(ctx, query)
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("GCSリスト取得失敗 (イテレーション中, URI: %s): %w", gcsURI, err)
		}

		// 区切り文字を指定したとき、疑似ディレクトリは Name が空で Prefix に入ります。
		name := attrs.Name
		if name == "" {
			name = attrs.Prefix
		}
		if name == "" || name == prefix {
			continue
		}
		if err := callback(remoteio.BuildGCSURI(bucketName, name)); err != nil {
			return err
		}
	}
	return nil
}

// Exists は GCS オブジェクトの存在を確認します。不在は (false, nil) を返します。
func (h *Handler) Exists(ctx context.Context, gcsURI string) (bool, error) {
	bucketName, objectName, err := h.parseObjectURI(gcsURI)
	if err != nil {
		return false, err
	}

	_, err = h.client.Bucket(bucketName).Object(objectName).Attrs(ctx)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, storage.ErrObjectNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("GCS属性取得失敗: %w", err)
}

// Write は GCS オブジェクトへ書き込みます。
func (h *Handler) Write(ctx context.Context, gcsURI string, contentReader io.Reader, settings remoteio.WriteSettings) error {
	bucketName, objectPath, err := h.parseObjectURI(gcsURI)
	if err != nil {
		return fmt.Errorf("GCSへの書き込みに失敗しました: %w", err)
	}

	slog.DebugContext(ctx, "GCS書き込み処理開始",
		slog.String("uri", gcsURI),
		slog.String("content_type", settings.ContentType),
		slog.String("disposition", settings.ContentDisposition),
		slog.String("cache_control", settings.CacheControl),
	)

	wc := h.client.Bucket(bucketName).Object(objectPath).NewWriter(ctx)
	wc.ContentType = settings.ContentType
	if settings.ContentDisposition != "" {
		wc.ContentDisposition = settings.ContentDisposition
	}
	if settings.CacheControl != "" {
		wc.CacheControl = settings.CacheControl
	}

	if _, err := io.Copy(wc, contentReader); err != nil {
		_ = wc.Close()
		return fmt.Errorf("GCSへのコンテンツ書き込み中にエラーが発生しました (URI: %s): %w", gcsURI, err)
	}

	if err := wc.Close(); err != nil {
		return fmt.Errorf("GCS Writerのクローズに失敗しました (URI: %s, アップロード処理中のエラー): %w", gcsURI, err)
	}

	slog.DebugContext(ctx, "GCS書き込み処理完了", slog.String("uri", gcsURI))
	return nil
}

// Delete は GCS オブジェクトを削除します。不在はエラーにしません。
func (h *Handler) Delete(ctx context.Context, gcsURI string) error {
	bucketName, objectPath, err := h.parseObjectURI(gcsURI)
	if err != nil {
		return err
	}
	err = h.client.Bucket(bucketName).Object(objectPath).Delete(ctx)
	if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return fmt.Errorf("GCSオブジェクトの削除に失敗しました: %w", err)
	}
	return nil
}

// parseObjectURI は URI を検証してバケット名とオブジェクト名に分解します。
//
// オブジェクト名が空の URI (gs://bucket) を拒否するのは、バケット操作と取り違えたり、
// 不在なのか URI が不正なのか区別できなくなるのを防ぐためです。
func (h *Handler) parseObjectURI(gcsURI string) (bucket, object string, err error) {
	if h.client == nil {
		return "", "", fmt.Errorf("GCSクライアントが未初期化です (URI: %s)", gcsURI)
	}
	bucket, object, err = remoteio.ParseRemoteURI(gcsURI)
	if err != nil {
		return "", "", err
	}
	if object == "" {
		return "", "", fmt.Errorf("オブジェクト名が空です: %s", gcsURI)
	}
	return bucket, object, nil
}
