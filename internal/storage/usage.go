package storage

import (
	"os"
	"path/filepath"
)

func DirUsed(root string) (bytes int64, files int) {
	if root == "" {
		return 0, 0
	}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
			files++
		}
		return nil
	})
	return bytes, files
}
