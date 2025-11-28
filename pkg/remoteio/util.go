package remoteio

import (
	"fmt"
	"strings"
)

// IsGCSURI は、URIが Google Cloud Storage (gs://) を指しているかどうかをチェックします。
func IsGCSURI(uri string) bool {
	return strings.HasPrefix(uri, "gs://")
}

// IsS3URI は、指定されたURIがS3 URI ("s3://...") であるかどうかをチェックします。
func IsS3URI(uri string) bool {
	return strings.HasPrefix(uri, "s3://")
}

// ParseGCSURI は、指定されたgs://URIをバケット名とオブジェクトパスにパースします。
// URIが "gs://" で始まっていない場合、または形式が正しくない場合はエラーを返します。
func ParseGCSURI(uri string) (bucketName string, objectPath string, err error) {
	if !IsGCSURI(uri) { // ★IsGCSURIを利用してチェックをリファクタ
		return "", "", fmt.Errorf("無効なGCS URI形式: 'gs://'で始まる必要があります")
	}

	path := uri[len("gs://"):] // ★定数またはlen()を使ってマジックナンバーを排除
	idx := strings.Index(path, "/")

	if idx == -1 {
		// "gs://bucket" の形式
		return path, "", nil
	}

	bucketName = path[:idx]
	objectPath = path[idx+1:]

	if bucketName == "" {
		return "", "", fmt.Errorf("GCS URIのバケット名が空です: %s", uri)
	}

	return bucketName, objectPath, nil
}

// ParseS3URI は、S3 URIをバケット名とオブジェクトパスに分割します。
// 例: "s3://my-bucket/path/to/object" -> ("my-bucket", "path/to/object", nil)
func ParseS3URI(s3URI string) (bucketName, objectPath string, err error) {
	if !IsS3URI(s3URI) {
		return "", "", fmt.Errorf("無効なS3 URI形式です: %s (s3:// で始まっていません)", s3URI)
	}

	path := s3URI[5:] // "s3://" を削除
	parts := strings.SplitN(path, "/", 2)

	if len(parts) == 0 || parts[0] == "" {
		return "", "", fmt.Errorf("無効なS3 URI形式です: %s (バケット名が空です)", s3URI)
	}

	bucketName = parts[0]
	objectPath = ""
	if len(parts) == 2 {
		objectPath = parts[1]
	}

	// S3では、オブジェクトパスが空（例: s3://bucket/）も許容される
	return bucketName, objectPath, nil
}
