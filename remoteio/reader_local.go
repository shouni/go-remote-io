package remoteio

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (r *UniversalInputReader) openLocal(path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ローカルファイルのオープンに失敗しました: %w", err)
	}
	return file, nil
}

func (r *UniversalInputReader) listLocal(path string, callback func(string) error, settings ListSettings) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("ローカルディレクトリの読み込みに失敗しました (path: %s): %w", path, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			// 区切り文字の指定が無いときの挙動は据え置きです。ローカルの一覧は元から
			// 直下のファイルだけを返しており、そこにディレクトリを混ぜると既存の
			// 呼び出し側が拾うものが変わってしまいます。
			if settings.Delimiter == "" {
				continue
			}
			if err := callback(filepath.Join(path, entry.Name()) + settings.Delimiter); err != nil {
				return err
			}
			continue
		}
		if err := callback(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (r *UniversalInputReader) existsLocal(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("ローカルファイルのステータス取得に失敗しました: %w", err)
}
