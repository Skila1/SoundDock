package artwork

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFolderArt(t *testing.T) {
	dir := t.TempDir()
	if FolderArt(dir) != "" {
		t.Fatal("empty dir")
	}
	p := filepath.Join(dir, "cover.jpg")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if FolderArt(dir) != p {
		t.Fatal(FolderArt(dir))
	}
}
