package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

func (r *UniversalInputReader) openGCS(ctx context.Context, gcsURI string) (io.ReadCloser, error) {
	if r.gcsClient == nil {
		return nil, fmt.Errorf("GCSクライアントが未初期化です (URI: %s)", gcsURI)
	}
	bucketName, objectName, err := ParseRemoteURI(gcsURI)
	if err != nil {
		return nil, err
	}
	if objectName == "" {
		return nil, fmt.Errorf("オブジェクト名が空です: %s", gcsURI)
	}

	rc, err := r.gcsClient.Bucket(bucketName).Object(objectName).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, fmt.Errorf("GCSオブジェクトが見つかりません (URI: %s): %w", gcsURI, os.ErrNotExist)
		}
		return nil, fmt.Errorf("GCS読み込み失敗 (URI: %s): %w", gcsURI, err)
	}
	return rc, nil
}

func (r *UniversalInputReader) listGCS(ctx context.Context, gcsURI string, callback func(string) error, cfg *listConfig) error {
	if r.gcsClient == nil {
		return fmt.Errorf("GCSクライアントが未初期化です (URI: %s)", gcsURI)
	}
	bucketName, prefix, err := ParseRemoteURI(gcsURI)
	if err != nil {
		return err
	}
	prefix = listPrefix(prefix, cfg)

	query := &storage.Query{Prefix: prefix, Delimiter: cfg.delimiter}
	it := r.gcsClient.Bucket(bucketName).Objects(ctx, query)
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
		if err := callback(BuildGCSURI(bucketName, name)); err != nil {
			return err
		}
	}
	return nil
}

func (r *UniversalInputReader) existsGCS(ctx context.Context, gcsURI string) (bool, error) {
	if r.gcsClient == nil {
		return false, fmt.Errorf("GCSクライアントが未初期化です: %s", gcsURI)
	}
	bucketName, objectName, err := ParseRemoteURI(gcsURI)
	if err != nil {
		return false, err
	}
	// バケット自体の存在確認と取り違えないよう、Open と同じ条件で弾きます。
	// オブジェクト名が空のまま Attrs を引くと、不在なのかURIが不正なのか区別できません。
	if objectName == "" {
		return false, fmt.Errorf("オブジェクト名が空です: %s", gcsURI)
	}

	_, err = r.gcsClient.Bucket(bucketName).Object(objectName).Attrs(ctx)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, storage.ErrObjectNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("GCS属性取得失敗: %w", err)
}
