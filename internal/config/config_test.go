package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMasterKeyFileWinsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SD_DATA_DIR", dir)
	t.Setenv("SD_MASTER_KEY", "from-env-should-lose")
	if err := os.WriteFile(filepath.Join(dir, "master.key"), []byte("from-file-wins\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Load()
	if cfg.MasterKey != "from-file-wins" {
		t.Fatalf("master key %q", cfg.MasterKey)
	}
	if cfg.LibraryHost != os.Getenv("SD_LIBRARY_HOST") {
		t.Fatalf("library host %q", cfg.LibraryHost)
	}
}

func TestMasterKeyEnvWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SD_DATA_DIR", dir)
	t.Setenv("SD_MASTER_KEY", "from-env-only")
	cfg := Load()
	if cfg.MasterKey != "from-env-only" {
		t.Fatalf("master key %q", cfg.MasterKey)
	}
}
