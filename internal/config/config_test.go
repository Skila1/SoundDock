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

func TestValidateRequiresSecureProductionSettings(t *testing.T) {
	t.Setenv("SD_ENV", "production")
	t.Setenv("SD_PUBLIC_URL", "https://music.example.com")
	t.Setenv("SD_MASTER_KEY", "short")
	t.Setenv("SD_COOKIE_SECURE", "false")
	if err := Load().Validate(); err == nil {
		t.Fatal("expected production validation failure")
	}

	t.Setenv("SD_MASTER_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("SD_COOKIE_SECURE", "true")
	t.Setenv("SD_DATABASE_URL", "postgres://user:pass@db.example.com:5432/sounddock?sslmode=require")
	if err := Load().Validate(); err != nil {
		t.Fatalf("expected production validation to pass: %v", err)
	}
}
