package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/uuid"
)

// Restore applies a previously created logical SQL dump with psql.
// Media libraries on disk/object storage are not rewritten.
func (s *Service) Restore(ctx context.Context, id uuid.UUID) error {
	rec, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	ok, detail := s.VerifyFile(rec.Path)
	if !ok {
		return fmt.Errorf("backup not restorable: %s", detail)
	}
	prev, err := s.Preview(ctx, id)
	if err != nil {
		return err
	}
	if prev.Empty || !prev.Readable {
		return fmt.Errorf("backup file is empty or unreadable")
	}
	if prev.RestoreKind != "sql" {
		return fmt.Errorf("only logical SQL dumps can be restored")
	}
	for _, w := range prev.Warnings {
		if strings.Contains(w, "incomplete") {
			return fmt.Errorf("refusing to restore an incomplete logical backup")
		}
	}
	if _, err := exec.LookPath("psql"); err != nil {
		return fmt.Errorf("psql is not available")
	}
	if _, err := os.Stat(rec.Path); err != nil {
		return err
	}
	s.setStatus(ctx, id, "restoring")
	cmd := exec.CommandContext(ctx, "psql", s.dsn, "-v", "ON_ERROR_STOP=1", "-f", rec.Path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.setStatus(ctx, id, "restore_failed")
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("restore failed: %s", msg)
	}
	s.setStatus(ctx, id, "restored")
	return nil
}
