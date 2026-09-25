package storage

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SanitizeKey rejects traversal, NUL, Windows alternate streams, and absolute paths.
func SanitizeKey(key string) (string, error) {
	key = strings.ReplaceAll(key, "\\", "/")
	if strings.ContainsRune(key, 0) {
		return "", ErrEscape
	}
	if strings.Contains(key, ":") {
		return "", ErrEscape
	}
	if path.IsAbs(key) || strings.HasPrefix(key, "/") {
		return "", ErrEscape
	}
	clean := path.Clean(key)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", ErrEscape
	}
	parts := strings.Split(clean, "/")
	for _, p := range parts {
		if p == ".." || p == "." && clean != "." {
			if p == ".." {
				return "", ErrEscape
			}
		}
	}
	if clean == "." {
		return "", nil
	}
	return strings.TrimPrefix(clean, "./"), nil
}

// Resolve under root. Rejects symlink escape via EvalSymlinks of the parent.
func ResolveUnder(root, key string) (string, error) {
	clean, err := SanitizeKey(key)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if realRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = realRoot
	}
	target := filepath.Join(absRoot, filepath.FromSlash(clean))
	for p := target; ; p = filepath.Dir(p) {
		if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", ErrEscape
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if p == absRoot || filepath.Dir(p) == p {
			break
		}
	}
	rel, err := filepath.Rel(absRoot, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrEscape
	}
	return target, nil
}
