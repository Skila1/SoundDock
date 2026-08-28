package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Restore decrypts, verifies, then wipe-and-applies. Passphrase is required.
// Wipe does not run until decrypt and checksum verify succeed.
func (s *Service) Restore(ctx context.Context, id uuid.UUID, passphrase string) (RestoreRequirements, error) {
	var empty RestoreRequirements
	if len([]rune(passphrase)) < MinPassphrase {
		return empty, ErrPassphraseShort
	}
	rec, err := s.Get(ctx, id)
	if err != nil {
		return empty, err
	}
	src := rec.Path
	if _, err := os.Stat(src); err != nil {
		return empty, fmt.Errorf("backup file is not on this machine")
	}

	prev, _ := s.loadState()
	st := RestoreState{Archive: src, Phase: PhaseDecrypt, Requirements: prev.Requirements}
	if prev.Archive == src {
		st.StartedAt = prev.StartedAt
	}
	_ = s.writeState(st)

	work, err := os.MkdirTemp("", "sounddock-restore-*")
	if err != nil {
		return empty, err
	}
	defer os.RemoveAll(work)
	st.WorkDir = work

	dek, master, err := s.unlockArchive(src, passphrase)
	if err != nil {
		st.Error = err.Error()
		_ = s.writeState(st)
		return empty, err
	}

	inner := src
	if isEncryptedArchive(src) {
		if err := s.setPhase(&st, PhaseDecrypt); err != nil {
			return empty, err
		}
		inner, err = decryptToFile(src, dek, filepath.Join(work, "inner.tar.gz"))
		if err != nil {
			st.Error = err.Error()
			_ = s.writeState(st)
			return empty, err
		}
	}

	unpacked := filepath.Join(work, "unpacked")
	sqlPath, mediaDir, err := unpackFullArchive(inner, unpacked)
	if err != nil {
		if !isEncryptedArchive(src) && !isGzip(src) {
			sqlPath = src
		} else {
			st.Error = err.Error()
			_ = s.writeState(st)
			return empty, err
		}
	}

	if err := s.setPhase(&st, PhaseVerify); err != nil {
		return empty, err
	}
	ok, detail := compareChecksums(unpacked)
	if !ok && isEncryptedArchive(src) {
		st.Error = detail
		_ = s.writeState(st)
		return empty, fmt.Errorf("backup not restorable: %s", detail)
	}
	if !ok && isGzip(src) {
		if _, err := os.Stat(filepath.Join(unpacked, "checksums.json")); err == nil {
			st.Error = detail
			_ = s.writeState(st)
			return empty, fmt.Errorf("backup not restorable: %s", detail)
		}
	}

	man, _ := readManifest(unpacked)
	head := s.imageSchema
	if head == 0 {
		head = ImageSchemaHead()
	}
	if man.SchemaVersion > head && man.SchemaVersion > 0 {
		err := fmt.Errorf("archive schema %d is newer than this image (%d)", man.SchemaVersion, head)
		st.Error = err.Error()
		_ = s.writeState(st)
		return empty, err
	}

	req, _ := readRequirements(unpacked)
	if req.InstanceName == "" {
		req.InstanceName = man.InstanceName
	}
	st.SchemaVersion = man.SchemaVersion
	st.Requirements = req

	skipDB := prev.Archive == src && (prev.Phase == PhaseFiles || prev.Phase == PhaseMasterKey)

	if _, err := exec.LookPath("psql"); err != nil && s.dumpFn == nil {
		return empty, fmt.Errorf("psql is not available")
	}

	if !skipDB {
		if err := s.setPhase(&st, PhaseWipe); err != nil {
			return empty, err
		}
		s.setStatus(ctx, id, "restoring")
		if err := s.wipeDatabase(ctx); err != nil {
			s.setStatus(ctx, id, "restore_failed")
			st.Error = err.Error()
			_ = s.writeState(st)
			return empty, err
		}
		if err := s.setPhase(&st, PhasePSQL); err != nil {
			return empty, err
		}
		if err := s.applySQL(ctx, sqlPath); err != nil {
			s.setStatus(ctx, id, "restore_failed")
			st.Error = err.Error()
			_ = s.writeState(st)
			return empty, err
		}
	}

	if err := s.setPhase(&st, PhaseFiles); err != nil {
		return empty, err
	}
	if mediaDir != "" && s.media != "" {
		if err := copyDir(mediaDir, s.media); err != nil {
			s.setStatus(ctx, id, "restore_failed")
			return empty, fmt.Errorf("database restored, media copy failed: %w", err)
		}
	}
	if s.artwork != "" {
		_ = copyDir(filepath.Join(unpacked, "artwork"), s.artwork)
	}
	if s.lyrics != "" {
		_ = copyDir(filepath.Join(unpacked, "lyrics"), s.lyrics)
	}

	if err := s.setPhase(&st, PhaseMasterKey); err != nil {
		return empty, err
	}
	if err := s.writeMasterKey(master); err != nil {
		s.setStatus(ctx, id, "restore_failed")
		return empty, err
	}

	req = req.AnnotateHost()
	st.Requirements = req
	_ = s.setPhase(&st, PhaseDone)
	s.setStatus(ctx, id, "restored")
	s.scheduleRestart()
	return req, nil
}

func (s *Service) unlockArchive(path, passphrase string) (dek, master []byte, err error) {
	if !isEncryptedArchive(path) {
		return nil, []byte(s.master), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	hdr, err := readClearHeader(f)
	if err != nil {
		return nil, nil, err
	}
	return unwrapRecovery(passphrase, hdr.Box, hdr.KDF)
}

func (s *Service) applySQL(ctx context.Context, sqlPath string) error {
	if s.dumpFn != nil && s.WipeFn != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "psql", s.dsn, "-v", "ON_ERROR_STOP=1", "-f", sqlPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("restore failed: %s", msg)
	}
	return nil
}

func (s *Service) writeMasterKey(master []byte) error {
	dir := s.dataDir
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, "master.key")
	return os.WriteFile(p, append(append([]byte{}, master...), '\n'), 0o600)
}

func (s *Service) scheduleRestart() {
	if s.Restart != nil {
		go s.Restart()
		return
	}
	go func() {
		time.Sleep(800 * time.Millisecond)
		os.Exit(0)
	}()
}

func copyDir(src, dest string) error {
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return nil
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return nil
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func (s *Service) ImportRemote(ctx context.Context, key string) (uuid.UUID, error) {
	st := s.LoadSettings(ctx)
	if !st.R2Enabled || st.Bucket == "" {
		return uuid.Nil, fmt.Errorf("R2 backup is not configured")
	}
	name := filepath.Base(strings.ReplaceAll(key, "\\", "/"))
	if name == "" || name == "." {
		return uuid.Nil, fmt.Errorf("invalid remote key")
	}
	dest := filepath.Join(s.dir, name)
	if err := s.DownloadRemote(ctx, st, key, dest); err != nil {
		return uuid.Nil, err
	}
	info, err := os.Stat(dest)
	if err != nil {
		return uuid.Nil, err
	}
	sum, _ := fileSHA(dest)
	kind := "full"
	if isGzip(dest) {
		kind = "full"
	} else if !isEncryptedArchive(dest) {
		kind = "sql"
	}
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO backups (path, size_bytes, checksum, status, destination, kind, remote_key)
		VALUES ($1,$2,$3,'created','r2',$4,$5) RETURNING id`, dest, info.Size(), sum, kind, key).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	ok, detail := s.VerifyFile(dest)
	_, _ = s.pool.Exec(ctx, `INSERT INTO backup_verifications (backup_id, ok, detail) VALUES ($1,$2,$3)`, id, ok, detail)
	if ok {
		_, _ = s.pool.Exec(ctx, `UPDATE backups SET status='verified' WHERE id=$1`, id)
	}
	return id, nil
}

func (s *Service) Requirements() (RestoreRequirements, error) {
	st, err := s.loadState()
	if err != nil {
		return RestoreRequirements{}, err
	}
	if st.Dismissed || st.Phase != PhaseDone {
		return RestoreRequirements{}, os.ErrNotExist
	}
	return st.Requirements.AnnotateHost(), nil
}
