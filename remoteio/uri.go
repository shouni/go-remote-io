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

// ParseBucketURI は、スキームを問わず「スキーム + バケット + キー」形式の URI を分解します。
// オブジェクトパスは空でも構いません（gs://bucket は ("gs://", "bucket", "") を返します）。
//
// スキームを固定しないのは、SchemeHandler と Router がスキーム非依存に作られている
// ためです。ここが gs:// と s3:// を直書きしていると、第三者が新しいスキームの
// ハンドラを書いたときに URI の分解だけ自前で再実装することになり、
// 「どこからがバケット名か」の解釈がこの関数とずれます。
func ParseBucketURI(uri string) (scheme, bucketName, objectPath string, err error) {
	scheme = SchemePrefix(uri)
	if scheme == "" {
		return "", "", "", fmt.Errorf("URIスキームがありません: %s", uri)
	}

	// スキーム削除後の文字列 (bucket/path/to/obj)
	body := uri[len(scheme):]
	if body == "" {
		return "", "", "", fmt.Errorf("バケット名が指定されていません: %s", uri)
	}

	bucketName, objectPath, _ = strings.Cut(body, "/")
	if bucketName == "" {
		return "", "", "", fmt.Errorf("バケット名が空です: %s", uri)
	}

	return scheme, bucketName, objectPath, nil
}

// ParseSchemeURI は、URI が指定スキームであることを確かめた上でバケット名とパスを返します。
// オブジェクトパスは空でも構いません（一覧のプレフィックス向け）。
//
// SchemeHandler の実装が、担当外のスキームを受け取ったまま処理してしまうのを防ぎます。
// Router 経由なら振り分けの時点で弾かれますが、ハンドラは公開されているため
// 直接呼ばれる余地があります。
func ParseSchemeURI(scheme, uri string) (bucketName, objectPath string, err error) {
	gotScheme, bucketName, objectPath, err := ParseBucketURI(uri)
	if err != nil {
		return "", "", err
	}
	if gotScheme != scheme {
		return "", "", fmt.Errorf("スキームが一致しません (期待: %s): %s", scheme, uri)
	}
	return bucketName, objectPath, nil
}

// ParseSchemeObjectURI は ParseSchemeURI に加えて、オブジェクト名が空でないことを検証します。
//
// オブジェクト名が空の URI (gs://bucket など) を拒否するのは、バケット操作と取り違えたり、
// 不在なのか URI が不正なのか区別できなくなるのを防ぐためです。
func ParseSchemeObjectURI(scheme, uri string) (bucketName, objectName string, err error) {
	bucketName, objectName, err = ParseSchemeURI(scheme, uri)
	if err != nil {
		return "", "", err
	}
	if objectName == "" {
		return "", "", fmt.Errorf("オブジェクト名が空です: %s", uri)
	}
	return bucketName, objectName, nil
}

// ParseRemoteURI は、URIのスキームを自動判別してバケット名とパスを返します。
// gs:// と s3:// のみを受け付けます。スキームを問わず分解したい場合は ParseBucketURI を使ってください。
func ParseRemoteURI(uri string) (bucketName, objectPath string, err error) {
	scheme, bucketName, objectPath, err := ParseBucketURI(uri)
	if err != nil {
		if SchemePrefix(uri) == "" {
			return "", "", fmt.Errorf("未対応のURIスキームです: %s", uri)
		}
		return "", "", err
	}
	if scheme != PrefixGCS && scheme != PrefixS3 {
		return "", "", fmt.Errorf("未対応のURIスキームです: %s", uri)
	}
	return bucketName, objectPath, nil
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

// BuildURI は、スキームを問わずバケット名とオブジェクトパスから URI を生成します。
// scheme は "gs://" のように区切りまで含めた形で渡します。
func BuildURI(scheme, bucketName, objectPath string) string {
	objectPath = strings.TrimPrefix(objectPath, "/")
	uri := scheme + bucketName

	if objectPath != "" {
		uri += "/" + objectPath
	}

	return uri
}

// BuildGCSURI は GCS 用のURIを作成します
func BuildGCSURI(bucketName, objectPath string) string {
	return BuildURI(PrefixGCS, bucketName, objectPath)
}

// BuildS3URI は S3 用のURIを作成します
func BuildS3URI(bucketName, objectPath string) string {
	return BuildURI(PrefixS3, bucketName, objectPath)
}
