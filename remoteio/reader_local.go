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

func (r *UniversalInputReader) listLocal(path string, callback func(string) error) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("ローカルディレクトリの読み込みに失敗しました (path: %s): %w", path, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			fullPath := filepath.Join(path, entry.Name())
			if err := callback(fullPath); err != nil {
				return err
			}
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
