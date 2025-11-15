package remoteio

import (
	"fmt"
	"strings"
)

// IsGCSURI は、URIが Google Cloud Storage (gs://) を指しているかどうかをチェックします。
func IsGCSURI(uri string) bool {
	return strings.HasPrefix(uri, "gs://")
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
