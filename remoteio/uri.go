package remoteio

import (
	"fmt"
	"path/filepath"
	"strings"
)

// スキームの語彙はここに集約します。
//
// 公開するのは区切りを含まない名前だけです。RFC 3986 でスキームと呼ぶのは "gs" の方で、
// 区切りを足す操作はここに閉じ込めます。呼び出し側が "://" を自前で連結する必要は
// ありません（連結を書かせると、足し忘れが "gsfoo://..." のような別スキームまで
// 前方一致で拾う形で表に出ます）。
const (
	// SchemeGCS は Google Cloud Storage のスキーム名です。
	SchemeGCS = "gs"
	// SchemeS3 は Amazon S3 のスキーム名です。
	SchemeS3 = "s3"
	// SchemeFile は file:// のスキーム名です。
	SchemeFile = "file"

	// schemeSeparator は URI のスキームと本体の区切りです。
	schemeSeparator = "://"
)

// Scheme は URI からスキーム名 ("gs" など) を取り出します。
// スキームを持たない文字列（ローカルパス）では空文字を返します。
//
// 判定は "://" の有無だけで行うため、"mailto:" のように二重スラッシュを持たない
// URI はローカルパスとして扱われます。
func Scheme(uri string) string {
	i := strings.Index(uri, schemeSeparator)
	if i <= 0 {
		return ""
	}
	return uri[:i]
}

// HasScheme は uri が指定スキームかどうかを返します。
//
// strings.HasPrefix(uri, "gs") のような素の前方一致だと "gsfoo://..." まで拾って
// しまうため、判定はこの関数を通してください。
func HasScheme(uri, scheme string) bool {
	return Scheme(uri) == scheme
}

// IsRemote は uri が gs:// または s3:// を指しているかどうかを返します。
func IsRemote(uri string) bool {
	switch Scheme(uri) {
	case SchemeGCS, SchemeS3:
		return true
	default:
		return false
	}
}

// BuildURI は、スキーム名・バケット名・オブジェクトパスから URI を組み立てます。
// scheme は区切りを含まない名前 ("gs") で渡します。
func BuildURI(scheme, bucket, object string) string {
	uri := scheme + schemeSeparator + bucket
	if object = strings.TrimPrefix(object, "/"); object != "" {
		uri += "/" + object
	}
	return uri
}

// ParseURI は、スキームを問わず「スキーム + バケット + オブジェクトパス」形式の
// URI を分解します。オブジェクトパスは空でも構いません
// （gs://bucket は ("gs", "bucket", "") を返します）。
//
// スキームを固定しないのは、Handler と Router がスキーム非依存に作られているためです。
// ここが gs:// と s3:// を直書きしていると、第三者が新しいスキームのハンドラを
// 書いたときに URI の分解だけ自前で再実装することになり、
// 「どこからがバケット名か」の解釈がこの関数とずれます。
func ParseURI(uri string) (scheme, bucket, object string, err error) {
	scheme = Scheme(uri)
	if scheme == "" {
		return "", "", "", fmt.Errorf("%w: スキームがありません (%s)", ErrInvalidURI, uri)
	}

	body := uri[len(scheme)+len(schemeSeparator):]
	bucket, object, _ = strings.Cut(body, "/")
	if bucket == "" {
		return "", "", "", fmt.Errorf("%w: バケット名が空です (%s)", ErrInvalidURI, uri)
	}
	return scheme, bucket, object, nil
}

// ParseBucketURI は、URI が指定スキームであることを確かめた上で
// バケット名とオブジェクトパスを返します。パスは空でも構いません（一覧向け）。
//
// ハンドラの実装が担当外のスキームを受け取ったまま処理してしまうのを防ぎます。
// Router 経由なら振り分けの時点で弾かれますが、ハンドラは公開されているため
// 直接呼ばれる余地があります。
func ParseBucketURI(scheme, uri string) (bucket, object string, err error) {
	got, bucket, object, err := ParseURI(uri)
	if err != nil {
		return "", "", err
	}
	if got != scheme {
		return "", "", fmt.Errorf("%w: スキームが一致しません (期待: %s, 実際: %s)", ErrInvalidURI, scheme, uri)
	}
	return bucket, object, nil
}

// ParseObjectURI は ParseBucketURI に加えて、オブジェクト名が空でないことを検証します。
//
// オブジェクト名が空の URI (gs://bucket など) を拒否するのは、バケット操作と
// 取り違えたり、不在なのか URI が不正なのか区別できなくなるのを防ぐためです。
func ParseObjectURI(scheme, uri string) (bucket, object string, err error) {
	bucket, object, err = ParseBucketURI(scheme, uri)
	if err != nil {
		return "", "", err
	}
	if object == "" {
		return "", "", fmt.Errorf("%w: オブジェクト名が空です (%s)", ErrInvalidURI, uri)
	}
	return bucket, object, nil
}

// NormalizeBucketName は、バケット「名」として受け取った値の表記ゆれを整えます。
// 前後の空白、既知のスキームプレフィックス、前後のスラッシュを落とします。
//
// 設定から読んだバケット名を BuildURI や Store.Sub へ渡す前に一度だけ通すことを
// 想定しています。素通しすると `gs://gs://my-bucket//path` のような URI ができ、
// 失敗するのは書き込みの時点になります。
func NormalizeBucketName(bucket string) string {
	bucket = strings.TrimSpace(bucket)
	// スキームの直書きを増やさないよう、既知の語彙から導出したプレフィックスで落とします。
	for _, scheme := range []string{SchemeGCS, SchemeS3, SchemeFile} {
		bucket = strings.TrimPrefix(bucket, scheme+schemeSeparator)
	}
	return strings.Trim(bucket, "/")
}

// ListPrefix は、一覧のプレフィックスを「ディレクトリの中身」を指す形へ正規化します。
// 既に区切りで終わっている場合と、バケット直下 (gs://bucket) の場合はそのまま返します。
//
// 常に正規化するのは、そうしないと同じ呼び出しがスキームによって別の意味になるためです。
// 正規化しなければ GCS / S3 では素の前方一致（`data` が `data-archive/` にも当たる）、
// ローカルはディレクトリ走査になります。
// Router が振り分け前に通しますが、ハンドラを直接呼ぶ場合のために公開しています。
func ListPrefix(uri string) string {
	if uri == "" || strings.HasSuffix(uri, "/") {
		return uri
	}
	// gs://bucket はバケット直下を指すため、そのままで既に「中身」の意味になります。
	if scheme := Scheme(uri); scheme != "" {
		if !strings.Contains(uri[len(scheme)+len(schemeSeparator):], "/") {
			return uri
		}
	}
	return uri + "/"
}

// Dir は、パスや URI から親ディレクトリを取り出し、末尾が区切りで終わる形に整えます。
//
// リモート URI を net/url で扱わないのは、gs:// / s3:// が URL ではないためです。
// オブジェクト名は任意のバイト列で、空白も ? も正当な文字ですが、URL として
// 組み直すとそれぞれ %20 とクエリ区切りに化けます。ParseURI はデコードしないため、
// 化けた URI はエラーにならずに別のキーを指してしまいます。
// 同じ理由で filepath.Dir もリモート URI には使いません（Windows でセパレータが混ざります）。
func Dir(uri string) string {
	if uri == "" {
		return ""
	}

	if scheme := Scheme(uri); scheme != "" {
		body := uri[len(scheme)+len(schemeSeparator):]
		if strings.HasSuffix(body, "/") {
			return uri
		}
		if before, _, ok := strings.CutLast(body, "/"); ok {
			return uri[:len(scheme)+len(schemeSeparator)] + before + "/"
		}
		// gs://bucket のようにキーを持たない URI。バケット直下を指します。
		return uri + "/"
	}

	baseDir := filepath.Dir(uri)
	if baseDir == "." {
		return "." + string(filepath.Separator)
	}
	if !strings.HasSuffix(baseDir, string(filepath.Separator)) {
		baseDir += string(filepath.Separator)
	}
	return baseDir
}

// Join は、ベースとなるパスや URI に名前を繋げます。
//
// スキーム付きなら "/" で、それ以外はローカルパスとして結合します。
// リモート側で filepath.Join も url.JoinPath も使わないのは、前者が連続するスラッシュを
// 畳んで `gs://bucket/a` を `gs:/bucket/a` に化けさせ、後者がオブジェクト名を
// パーセントエンコードしてしまうためです（Dir のコメント参照）。
func Join(base, name string) string {
	if Scheme(base) == "" && base != "" {
		return filepath.Join(base, name)
	}
	base = strings.TrimSuffix(base, "/")
	if name = strings.TrimPrefix(name, "/"); name == "" {
		return base
	}
	if base == "" {
		return name
	}
	return base + "/" + name
}

// IndexedPath は、パスの拡張子の前に連番を挿入します。
// 例: "path/to/image.png", 1 -> "path/to/image_1.png"
//
// 拡張子は最後の区切りより後ろの最後の "." 以降を指します（filepath.Ext と同じ）。
// リモート URI もローカルパスも同じ規則で扱うため、"gs://b/dir.v2/name" のように
// ディレクトリ側にだけ "." がある場合は拡張子なしとして扱われます。
func IndexedPath(base string, index int) (string, error) {
	if index <= 0 {
		return "", fmt.Errorf("%w: インデックスは1以上の整数である必要があります (%d)", ErrInvalidURI, index)
	}
	ext := filepath.Ext(base)
	return fmt.Sprintf("%s_%d%s", strings.TrimSuffix(base, ext), index, ext), nil
}
