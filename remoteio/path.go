package remoteio

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// ResolveBaseDir は、入力パスから親ディレクトリのパスを抽出し、
// 末尾がセパレータ（URL なら /、ローカルなら OS 依存）で終わるように正規化します。
func ResolveBaseDir(rawPath string) string {
	if rawPath == "" {
		return ""
	}

	u, err := url.Parse(rawPath)
	// スキームがある場合は URL として処理
	if err == nil && u.Scheme != "" {
		// ディレクトリ構造を取得するためにパスの末尾を調整
		if !strings.HasSuffix(u.Path, "/") {
			u.Path = filepath.Dir(u.Path)
		}
		baseURL := u.String()
		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}
		return baseURL
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
// スキーム付き（gs:// / s3:// / http:// など）なら URL として、それ以外はローカルパスとして
// 結合します。判定に SchemePrefix を使うのは、ローカル用の filepath.Join が連続する
// スラッシュを畳んでしまい、`gs://bucket/a` が `gs:/bucket/a` に化けるためです。
func ResolvePath(baseDir, fileName string) (string, error) {
	if SchemePrefix(baseDir) != "" {
		result, err := url.JoinPath(baseDir, fileName)
		if err != nil {
			return "", fmt.Errorf("リモートストレージパスの結合に失敗: %w", err)
		}
		return result, nil
	}

	return filepath.Join(baseDir, fileName), nil
}

// GenerateIndexedPath は、指定されたパスの拡張子の前に連番を挿入します。
// 例: "path/to/image.png", 1 -> "path/to/image_1.png"
func GenerateIndexedPath(basePath string, index int) (string, error) {
	if index <= 0 {
		return "", fmt.Errorf("インデックスは1以上の整数である必要があります: %d", index)
	}

	// URL の場合は Path 部分のみ、ローカルなら全体から拡張子を取得
	ext := filepath.Ext(basePath)
	mainPath := strings.TrimSuffix(basePath, ext)

	return fmt.Sprintf("%s_%d%s", mainPath, index, ext), nil
}
