package remoteio

import (
	"fmt"
	"strings"
)

const (
	// PrefixGCS は Google Cloud Storage の URI スキームプレフィックスです。
	PrefixGCS = "gs://"
	// PrefixS3 は Amazon S3 の URI スキームプレフィックスです。
	PrefixS3 = "s3://"
)

// IsRemoteURI は対応しているいずれかのスキームであれば true を返します
func IsRemoteURI(uri string) bool {
	_, _, err := ParseRemoteURI(uri)
	return err == nil
}

// IsGCSURI は、URIが Google Cloud Storage を指しているかどうかをチェックします。
func IsGCSURI(uri string) bool {
	return strings.HasPrefix(uri, PrefixGCS)
}

// IsS3URI は、指定されたURIが S3 を指しているかどうかをチェックします。
func IsS3URI(uri string) bool {
	return strings.HasPrefix(uri, PrefixS3)
}

// ParseRemoteURI は、URIのスキームを自動判別してバケット名とパスを返します。
func ParseRemoteURI(uri string) (bucketName, objectPath string, err error) {
	if strings.HasPrefix(uri, PrefixGCS) {
		return parseCloudStorageURI(uri, PrefixGCS)
	}
	if strings.HasPrefix(uri, PrefixS3) {
		return parseCloudStorageURI(uri, PrefixS3)
	}
	return "", "", fmt.Errorf("未対応のURIスキームです: %s", uri)
}

// NormalizeBucketName は、バケット「名」として受け取った値の表記ゆれを整えます。
// 前後の空白、スキームプレフィックス (gs:// / s3://)、前後のスラッシュを落とします。
//
// 設定から読んだバケット名を BuildGCSURI / BuildS3URI へ渡す前に通すことを想定しています。
// これらは受け取った値をそのまま連結するため、コンソールから貼った `gs://my-bucket/` の
// ような値を素通しすると `gs://gs://my-bucket//path` という URI を組み立ててしまい、
// 失敗するのは書き込みの時点になります。
//
// オブジェクトパスまで含む URI は分解しません（`gs://b/a/b.txt` は `b/a/b.txt` のまま
// 返ります）。バケットとパスを分けたい場合は ParseRemoteURI を使ってください。
func NormalizeBucketName(bucket string) string {
	bucket = strings.TrimSpace(bucket)
	bucket = strings.TrimPrefix(bucket, PrefixGCS)
	bucket = strings.TrimPrefix(bucket, PrefixS3)

	return strings.Trim(bucket, "/")
}

// BuildGCSURI は GCS 用のURIを作成します
func BuildGCSURI(bucketName, objectPath string) string {
	return buildRemoteURI(PrefixGCS, bucketName, objectPath)
}

// BuildS3URI は S3 用のURIを作成します
func BuildS3URI(bucketName, objectPath string) string {
	return buildRemoteURI(PrefixS3, bucketName, objectPath)
}

// buildRemoteURI は、指定されたスキーム、バケット名、オブジェクトパスからURIを生成します。
func buildRemoteURI(prefix, bucketName, objectPath string) string {
	objectPath = strings.TrimPrefix(objectPath, "/")
	uri := prefix + bucketName

	if objectPath != "" {
		uri += "/" + objectPath
	}

	return uri
}

// ParseRemoteURI は、URIのスキームを自動判別してバケット名とパスを返します。
func parseCloudStorageURI(uri, prefix string) (string, string, error) {
	if !strings.HasPrefix(uri, prefix) {
		return "", "", fmt.Errorf("無効なURI形式: '%s'で始まる必要があります", prefix)
	}

	// スキーム削除後の文字列 (bucket/path/to/obj)
	body := uri[len(prefix):]
	if body == "" {
		return "", "", fmt.Errorf("バケット名が指定されていません: %s", uri)
	}

	parts := strings.SplitN(body, "/", 2)
	bucketName := parts[0]
	if bucketName == "" {
		return "", "", fmt.Errorf("バケット名が空です: %s", uri)
	}

	var objectPath string
	if len(parts) == 2 {
		objectPath = parts[1]
	}

	return bucketName, objectPath, nil
}
