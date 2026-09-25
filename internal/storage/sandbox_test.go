package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSanitizeKey(t *testing.T) {
	ok := []string{"a/b.mp3", "Artist/Album/01 - Track.flac", "nested/dir/file.ogg"}
	for _, k := range ok {
		if _, err := SanitizeKey(k); err != nil {
			t.Fatalf("%q: %v", k, err)
		}
	}
	bad := []string{"../etc/passwd", "/etc/passwd", "a/../../secret", "foo\x00bar", `C:\Windows\x.mp3`, "file:stream"}
	for _, k := range bad {
		if _, err := SanitizeKey(k); err == nil {
			t.Fatalf("expected reject %q", k)
		}
	}
}

func TestResolveUnder(t *testing.T) {
	root := t.TempDir()
	p, err := ResolveUnder(root, "music/a.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if p == "" {
		t.Fatal("empty")
	}
	if _, err := ResolveUnder(root, "../outside"); err == nil {
		t.Fatal("expected escape")
	}
	base := filepath.Join(root, "mount")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(base, "escape")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink test requires Windows Developer Mode or elevated privileges: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := ResolveUnder(root, filepath.Join("mount", "escape", "secret.mp3")); err == nil {
		t.Fatal("expected symlink escape rejection")
	}
}
