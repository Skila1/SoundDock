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

func CanApply() bool {
	return HelperOK() || SocketOK()
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

func AppliedHealthy() bool {
	b, err := os.ReadFile(filepath.Join(RequestDir(), "healthy"))
	if err != nil {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(string(b)))
	return s == "1" || s == "ok" || s == "true"
}

type RecoveryFile struct {
	Status string `json:"status"`
	Backup string `json:"backup"`
	Detail string `json:"detail"`
}

func ReadRecovery() RecoveryFile {
	var out RecoveryFile
	b, err := os.ReadFile(filepath.Join(RequestDir(), "needs_recovery"))
	if err != nil || len(b) == 0 {
		return out
	}
	if json.Unmarshal(b, &out) != nil {
		out.Status = "needs_recovery"
		out.Detail = strings.TrimSpace(string(b))
	}
	if out.Status == "" {
		out.Status = "needs_recovery"
	}
	return out
}

func WriteRecovery(status, backup, detail string) {
	ensureUpdateDir()
	b, _ := json.Marshal(RecoveryFile{Status: status, Backup: backup, Detail: detail})
	_ = os.WriteFile(filepath.Join(RequestDir(), "needs_recovery"), append(b, '\n'), 0o660)
	writeProgress(0, "needs_recovery", detail)
}

func ensureUpdateDir() error {
	dir := RequestDir()
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o2770)
	return nil
}

func RequestPending() bool {
	st, err := os.Stat(filepath.Join(RequestDir(), "request"))
	if err != nil {
		return false
	}
	if time.Since(st.ModTime()) > 30*time.Minute {
		_ = os.Remove(filepath.Join(RequestDir(), "request"))
		return false
	}
	return true
}

func ClearRequest() {
	_ = os.Remove(filepath.Join(RequestDir(), "request"))
	_ = os.Remove(filepath.Join(RequestDir(), "request.tmp"))
}

func WriteHostRunner() error {
	if err := ensureUpdateDir(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(RequestDir(), "run.sh"), hostUpdateScript, 0o755)
}

func writeProgress(percent int, stage, detail string) {
	_ = ensureUpdateDir()
	b, _ := json.Marshal(hostProgressFile{Percent: percent, Stage: stage, Detail: detail})
	tmp := filepath.Join(RequestDir(), "progress.json.tmp")
	dst := filepath.Join(RequestDir(), "progress.json")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o660); err != nil {
		return
	}
	_ = os.Rename(tmp, dst)
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
	// Remove first so systemd PathExists sees a create. Overlay/bind-mount
	// inotify often misses container writes; the timer + docker socket cover that.
	_ = os.Remove(dst)
	if err := os.WriteFile(tmp, payload, 0o660); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	writeProgress(5, "queued", "Waiting for the host helper")
	return nil
}
