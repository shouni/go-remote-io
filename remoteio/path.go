package remoteio

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveBaseDir は、入力パスから親ディレクトリのパスを抽出し、
// 末尾がセパレータ（リモートなら /、ローカルなら OS 依存）で終わるように正規化します。
//
// リモート URI を net/url で扱わないのは、gs:// / s3:// が URL ではないためです。
// オブジェクト名は任意のバイト列で、空白も ? も正当な文字ですが、URL として
// 組み直すとそれぞれ %20 とクエリ区切りに化けます。ParseRemoteURI はデコードを
// しないため、化けた URI はエラーにならずに別のキーを指してしまいます。
//
// スキームの判定は SchemePrefix と同じく "://" の有無で行うため、"mailto:" のような
// 二重スラッシュを持たない URI はローカルパスとして扱われます。
func ResolveBaseDir(rawPath string) string {
	if rawPath == "" {
		return ""
	}

	if scheme := SchemePrefix(rawPath); scheme != "" {
		body := rawPath[len(scheme):]
		if strings.HasSuffix(body, "/") {
			return rawPath
		}
		i := strings.LastIndex(body, "/")
		if i < 0 {
			// gs://bucket のようにキーを持たない URI。バケット直下を指します。
			return rawPath + "/"
		}
		return scheme + body[:i+1]
	}

	// ローカルファイルパスとして処理
	baseDir := filepath.Dir(rawPath)
	if baseDir == "." {
		return "." + string(filepath.Separator)
	}

	if !strings.HasSuffix(baseDir, string(filepath.Separator)) {
		baseDir += string(filepath.Separator)
	}
	return baseDir
}

// ResolvePath は、ベースディレクトリとファイル名を結合します。
//
// スキーム付き（gs:// / s3:// など）なら "/" で、それ以外はローカルパスとして結合します。
// リモート側で filepath.Join も url.JoinPath も使わないのは、前者が連続するスラッシュを
// 畳んで `gs://bucket/a` を `gs:/bucket/a` に化けさせ、後者がオブジェクト名を
// パーセントエンコードしてしまうためです（ResolveBaseDir のコメント参照）。
//
// 戻り値のエラーは常に nil ですが、シグネチャは互換のために残しています。
func ResolvePath(baseDir, fileName string) (string, error) {
	if SchemePrefix(baseDir) != "" {
		base := strings.TrimSuffix(baseDir, "/")
		name := strings.TrimPrefix(fileName, "/")
		if name == "" {
			return base, nil
		}
		return base + "/" + name, nil
	}

	return filepath.Join(baseDir, fileName), nil
}

// GenerateIndexedPath は、指定されたパスの拡張子の前に連番を挿入します。
// 例: "path/to/image.png", 1 -> "path/to/image_1.png"
//
// 拡張子は最後のセパレータより後ろの最後の "." 以降を指します（filepath.Ext と同じ）。
// リモート URI もローカルパスも同じ規則で扱うため、"gs://b/dir.v2/name" のように
// ディレクトリ側にだけ "." がある場合は拡張子なしとして扱われます。
func GenerateIndexedPath(basePath string, index int) (string, error) {
	if index <= 0 {
		return "", fmt.Errorf("インデックスは1以上の整数である必要があります: %d", index)
	}

	ext := filepath.Ext(basePath)
	mainPath := strings.TrimSuffix(basePath, ext)

	return fmt.Sprintf("%s_%d%s", mainPath, index, ext), nil
}
