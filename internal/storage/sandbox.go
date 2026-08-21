package storage

import (
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// SanitizeKey rejects traversal, NUL, Windows alternate streams, and absolute paths.
func SanitizeKey(key string) (string, error) {
	key = strings.ReplaceAll(key, "\\", "/")
	if strings.ContainsRune(key, 0) {
		return "", ErrEscape
	}
	if strings.Contains(key, ":") && runtime.GOOS == "windows" {
		// Drive letters and ADS (file:stream)
		if len(key) >= 2 && key[1] == ':' {
			return "", ErrEscape
		}
		if strings.Contains(key, ":") {
			return "", ErrEscape
		}
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
	target := filepath.Join(absRoot, filepath.FromSlash(clean))
	rel, err := filepath.Rel(absRoot, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrEscape
	}
	return target, nil
}
