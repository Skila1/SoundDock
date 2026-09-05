package lint

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// Literal U+2014 in authored files is banned. Source that writes `\u2014` as an
// escape (for example internal/metadata/filename.go) is ASCII and is not flagged.
var emDash = []byte("\u2014")

func TestNoAuthoredEmDash(t *testing.T) {
	root := repoRoot(t)
	skipDir := map[string]bool{
		".git": true, "node_modules": true, "dist": true, "data": true,
		"bin": true, ".cursor": true, "third_party": true, "vendor": true,
	}
	var hits []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if skipDir[name] {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if shouldSkipFile(rel) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(b) {
			return nil
		}
		if bytes.Contains(b, emDash) {
			hits = append(hits, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Fatalf("authored em dash (U+2014) in: %s", strings.Join(hits, ", "))
	}
}

func shouldSkipFile(rel string) bool {
	base := filepath.Base(rel)
	switch filepath.Ext(base) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".woff", ".woff2", ".ico", ".bin":
		return true
	}
	return false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
