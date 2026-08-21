package update

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed host-update.sh
var hostUpdateScript []byte

func RequestDir() string {
	if v := strings.TrimSpace(os.Getenv("SD_UPDATE_DIR")); v != "" {
		return v
	}
	return "/update"
}

func HelperOK() bool {
	dir := RequestDir()
	if _, err := os.Stat(filepath.Join(dir, "helper")); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".writable")
	if err := os.WriteFile(probe, []byte("1"), 0o644); err != nil {
		return false
	}
	_ = os.Remove(probe)
	return true
}

func AppliedDigest() string {
	b, err := os.ReadFile(filepath.Join(RequestDir(), "applied"))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if i := strings.LastIndex(s, "@"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func RequestPending() bool {
	st, err := os.Stat(filepath.Join(RequestDir(), "request"))
	if err != nil {
		return false
	}
	if time.Since(st.ModTime()) > 10*time.Minute {
		_ = os.Remove(filepath.Join(RequestDir(), "request"))
		return false
	}
	return true
}

func WriteHostRunner() error {
	dir := RequestDir()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "run.sh"), hostUpdateScript, 0o755)
}

func RequestUpdate(by string) error {
	if !HelperOK() {
		return fmt.Errorf("host update helper is not available")
	}
	_ = WriteHostRunner()
	dir := RequestDir()
	payload, _ := json.Marshal(map[string]string{
		"at":    time.Now().UTC().Format(time.RFC3339),
		"by":    by,
		"image": ImageRef(),
	})
	tmp := filepath.Join(dir, "request.tmp")
	dst := filepath.Join(dir, "request")
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
