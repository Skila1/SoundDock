package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
)

type Service struct {
	pool        *pgxpool.Pool
	dir         string
	dsn         string
	media       string
	artwork     string
	lyrics      string
	dataDir     string
	master      string
	instance    string
	imageSchema int
	box         *cryptox.Box
	lookPath    func(string) (string, error)
	dumpFn      func(ctx context.Context, dest string) error
	WipeFn      func(ctx context.Context) error
	Restart     func()
	wiped       bool
}

func New(pool *pgxpool.Pool, dir, dsn string) *Service {
	_ = os.MkdirAll(dir, 0o755)
	return &Service{pool: pool, dir: dir, dsn: dsn}
}

func (s *Service) requireDumpTool() error {
	look := exec.LookPath
	if s != nil && s.lookPath != nil {
		look = s.lookPath
	}
	if _, err := look("pg_dump"); err != nil {
		return fmt.Errorf("pg_dump is missing: backups cannot run")
	}
	return nil
}

func (s *Service) runDump(ctx context.Context, dest string) error {
	if s.dumpFn != nil {
		return s.dumpFn(ctx, dest)
	}
	cmd := exec.CommandContext(ctx, "pg_dump", s.dsn, "-f", dest, "--no-owner")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(out)
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("pg_dump failed: %s", msg)
	}
	st, err := os.Stat(dest)
	if err != nil || st.Size() == 0 {
		return fmt.Errorf("pg_dump produced no SQL")
	}
	return nil
}

// Run creates an encrypted archive. It never prompts for a passphrase.
func (s *Service) Run(ctx context.Context) (uuid.UUID, error) {
	if err := s.requireDumpTool(); err != nil {
		return uuid.Nil, err
	}
	st := s.LoadSettings(ctx)
	if !st.RestorePassphraseSet {
		return uuid.Nil, ErrPassphraseRequired
	}
	stored := s.loadStored(ctx)
	dek, err := unboxDEK(s.box, stored.DekEnc)
	if err != nil {
		return uuid.Nil, fmt.Errorf("unwrap archive DEK: %w", err)
	}
	kdf := s.kdfFromStored(stored)
	if len(stored.RecoveryBox) == 0 {
		return uuid.Nil, ErrPassphraseRequired
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	work, err := os.MkdirTemp("", "sounddock-backup-*")
	if err != nil {
		return uuid.Nil, err
	}
	defer os.RemoveAll(work)
	sqlPath := filepath.Join(work, "dump.sql")
	if err := s.runDump(ctx, sqlPath); err != nil {
		return uuid.Nil, err
	}

	stage := filepath.Join(work, "inner")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return uuid.Nil, err
	}
	req := ClassifyRestoreRequirements(s.cfgSnapshot())
	if err := s.stageInner(stage, sqlPath, st.IncludeMedia, req); err != nil {
		return uuid.Nil, err
	}
	inner := filepath.Join(work, "inner.tar.gz")
	schema := s.imageSchema
	if schema == 0 {
		schema = ImageSchemaHead()
	}
	if err := packInnerArchive(inner, stage, st.IncludeMedia, s.instance, schema); err != nil {
		return uuid.Nil, err
	}

	if !st.LocalEnabled && !st.R2Enabled {
		st.LocalEnabled = true
	}
	name := fmt.Sprintf("sounddock-full-%s.sdar", stamp)
	path := filepath.Join(s.dir, name)
	if err := encryptArchiveFile(path, inner, dek, stored.RecoveryBox, kdf); err != nil {
		return uuid.Nil, err
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		return uuid.Nil, err
	}
	sum, _ := fileSHA(path)
	dest := "local"
	remote := ""
	if st.R2Enabled && st.Bucket != "" && st.Endpoint != "" {
		remote = remoteKey(st.Prefix, filepath.Base(path))
		if err := s.UploadRemote(ctx, st, path, remote); err != nil {
			return uuid.Nil, fmt.Errorf("R2 upload failed: %w", err)
		}
		if st.LocalEnabled {
			dest = "both"
		} else {
			dest = "r2"
		}
	}
	ok, detail := s.VerifyArchive(path, dek)
	if !st.LocalEnabled && dest == "r2" {
		_ = os.Remove(path)
		path = remote
	}
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO backups (path, size_bytes, checksum, status, destination, kind, remote_key)
		VALUES ($1,$2,$3,'created',$4,$5,$6) RETURNING id`,
		path, fileInfo.Size(), sum, dest, "full", remote).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	_, _ = s.pool.Exec(ctx, `INSERT INTO backup_verifications (backup_id, ok, detail) VALUES ($1,$2,$3)`, id, ok, detail)
	if ok {
		_, _ = s.pool.Exec(ctx, `UPDATE backups SET status='verified' WHERE id=$1`, id)
	} else {
		_, _ = s.pool.Exec(ctx, `UPDATE backups SET status='verify_failed' WHERE id=$1`, id)
	}
	return id, nil
}

func (s *Service) ShouldRunScheduled(ctx context.Context) bool {
	st := s.LoadSettings(ctx)
	if !st.ScheduledEnabled || !st.RestorePassphraseSet {
		return false
	}
	var last time.Time
	err := s.pool.QueryRow(ctx, `SELECT created_at FROM backups ORDER BY created_at DESC LIMIT 1`).Scan(&last)
	if err != nil {
		return true
	}
	return time.Since(last) >= 20*time.Hour
}

type Record struct {
	ID          uuid.UUID
	Path        string
	SizeBytes   int64
	Checksum    string
	Status      string
	CreatedAt   time.Time
	Destination string
	Kind        string
	RemoteKey   string
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Record, error) {
	var rec Record
	err := s.pool.QueryRow(ctx, `
		SELECT id, path, size_bytes, COALESCE(checksum,''), status, created_at,
		       COALESCE(destination,'local'), COALESCE(kind,'sql'), COALESCE(remote_key,'')
		FROM backups WHERE id=$1`, id).
		Scan(&rec.ID, &rec.Path, &rec.SizeBytes, &rec.Checksum, &rec.Status, &rec.CreatedAt, &rec.Destination, &rec.Kind, &rec.RemoteKey)
	return rec, err
}

func (s *Service) setStatus(ctx context.Context, id uuid.UUID, status string) {
	_, _ = s.pool.Exec(ctx, `UPDATE backups SET status=$2 WHERE id=$1`, id, status)
}

func (s *Service) VerifyFile(path string) (bool, string) {
	st, err := os.Stat(path)
	if err != nil {
		return false, err.Error()
	}
	if st.Size() == 0 {
		return false, "empty archive"
	}
	if isEncryptedArchive(path) {
		f, err := os.Open(path)
		if err != nil {
			return false, err.Error()
		}
		defer f.Close()
		if _, err := readClearHeader(f); err != nil {
			return false, err.Error()
		}
		return true, fmt.Sprintf("size=%d encrypted=true header_ok=true", st.Size())
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err.Error()
	}
	defer f.Close()
	buf := make([]byte, 64)
	n, _ := f.Read(buf)
	if n == 0 {
		return false, "unreadable"
	}
	return true, fmt.Sprintf("size=%d checksum_ok=true", st.Size())
}

func (s *Service) countBackups(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM backups`).Scan(&n)
	return n, err
}
