package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func DumpSQL(ctx context.Context, dsn, destDir string) (string, error) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return "", fmt.Errorf("pg_dump is required before a schema-forward update")
	}
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("SD_DATABASE_URL"))
	}
	if dsn == "" {
		return "", fmt.Errorf("database URL is required for a pre-update SQL backup")
	}
	if destDir == "" {
		destDir = filepath.Join(RequestDir(), "backups")
	}
	if err := os.MkdirAll(destDir, 0o770); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, "pre-update-"+time.Now().UTC().Format("20060102-150405")+".sql")
	cmd := exec.CommandContext(ctx, "pg_dump", dsn, "-f", dest, "--no-owner")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(dest)
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("pg_dump failed: %s", redactDumpErr(msg))
	}
	info, err := os.Stat(dest)
	if err != nil || info.Size() < 64 {
		_ = os.Remove(dest)
		return "", fmt.Errorf("pg_dump produced an empty backup")
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		return "", err
	}
	if strings.Contains(string(raw), "pg_dump unavailable") {
		_ = os.Remove(dest)
		return "", fmt.Errorf("pg_dump produced an incomplete backup")
	}
	return dest, nil
}

func redactDumpErr(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if i := strings.Index(strings.ToLower(s), "password"); i >= 0 {
		return "pg_dump could not connect"
	}
	if len(s) > 180 {
		return s[:180]
	}
	return s
}
