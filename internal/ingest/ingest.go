package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/scan"
	"github.com/sounddock/sounddock/internal/storage"
)

var ingestSem = make(chan struct{}, 16)

type Service struct {
	pool     *pgxpool.Pool
	managed  *storage.Local
	scanner  *scan.Scanner
	staging  string
	maxBytes int64
}

func New(pool *pgxpool.Pool, managed *storage.Local, scanner *scan.Scanner, staging string, maxBytes int64) *Service {
	_ = os.MkdirAll(staging, 0o755)
	if maxBytes <= 0 {
		maxBytes = 200 << 20
	}
	return &Service{pool: pool, managed: managed, scanner: scanner, staging: staging, maxBytes: maxBytes}
}

type URLPayload struct {
	URL       string    `json:"url"`
	Extra     []string  `json:"urls"`
	LibraryID uuid.UUID `json:"library_id"`
}

type UploadComplete struct {
	SessionID uuid.UUID `json:"session_id"`
}

func (s *Service) CreateUpload(ctx context.Context, user, lib uuid.UUID, filename string, size int64) (uuid.UUID, string, error) {
	if !scan.IsUploadName(filename) {
		return uuid.Nil, "", fmt.Errorf("unsupported audio type")
	}
	if lib == uuid.Nil {
		return uuid.Nil, "", fmt.Errorf("library is required")
	}
	id := uuid.New()
	key := filepath.Join(s.staging, id.String())
	f, err := os.Create(key)
	if err != nil {
		return uuid.Nil, "", err
	}
	f.Close()
	_, err = s.pool.Exec(ctx, `INSERT INTO upload_sessions (id, user_id, library_id, filename, size_bytes, staging_key) VALUES ($1,$2,$3,$4,$5,$6)`,
		id, user, lib, filename, size, key)
	return id, id.String(), err
}

func (s *Service) PatchUpload(ctx context.Context, id uuid.UUID, offset int64, r io.Reader) (int64, error) {
	var staging string
	var size int64
	if err := s.pool.QueryRow(ctx, `SELECT staging_key, size_bytes FROM upload_sessions WHERE id=$1`, id).Scan(&staging, &size); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(staging, os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	newOff := offset + n
	_, _ = s.pool.Exec(ctx, `UPDATE upload_sessions SET offset_bytes=$2, updated_at=now() WHERE id=$1`, id, newOff)
	return newOff, err
}

func (s *Service) FinishUpload(ctx context.Context, id uuid.UUID, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error), doScan bool) error {
	var staging, filename string
	var lib uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT staging_key, filename, library_id FROM upload_sessions WHERE id=$1`, id).Scan(&staging, &filename, &lib); err != nil {
		return err
	}
	if scan.IsZipName(filename) {
		if s.pool != nil {
			_, _ = s.pool.Exec(ctx, `UPDATE upload_sessions SET state='queued' WHERE id=$1`, id)
		}
		return ErrZipQueued
	}
	f, err := os.Open(staging)
	if err != nil {
		return err
	}
	hw := sha256.New()
	if _, err := io.Copy(hw, f); err != nil {
		f.Close()
		return err
	}
	hash := hex.EncodeToString(hw.Sum(nil))
	f.Seek(0, io.SeekStart)
	var exist string
	if err := s.pool.QueryRow(ctx, `SELECT storage_key FROM track_files WHERE content_hash=$1 LIMIT 1`, hash).Scan(&exist); err == nil {
		f.Close()
		os.Remove(staging)
		_, _ = s.pool.Exec(ctx, `UPDATE upload_sessions SET state='duplicate', content_hash=$2 WHERE id=$1`, id, hash)
		return nil
	}
	prov, libID, prefix, err := getProv(ctx, lib)
	if err != nil {
		f.Close()
		return err
	}
	key := path.Join(prefix, "uploads", hash[:2], hash+path.Ext(filename))
	st, _ := f.Stat()
	err = prov.Write(ctx, key, f, storage.WriteInfo{Size: st.Size()})
	f.Close()
	if err != nil {
		return err
	}
	os.Remove(staging)
	_, _ = s.pool.Exec(ctx, `UPDATE upload_sessions SET state='complete', content_hash=$2 WHERE id=$1`, id, hash)
	_ = doScan
	if s.scanner == nil {
		return nil
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		ingestSem <- struct{}{}
		defer func() { <-ingestSem }()
		_ = s.scanner.IngestKey(ctx, libID, prov, key, filename)
	}()
	return nil
}

func (s *Service) ScanUploads(ctx context.Context, lib uuid.UUID, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) error {
	prov, libID, prefix, err := getProv(ctx, lib)
	if err != nil {
		return err
	}
	return s.scanner.ScanLibrary(ctx, libID, prov, path.Join(prefix, "uploads"), "upload", uuid.Nil)
}

type MigratePayload struct {
	Source         uuid.UUID `json:"source_library_id"`
	Dest           uuid.UUID `json:"dest_library_id"`
	Mode           string    `json:"mode"` // requested copy|move
	RequestedMode  string    `json:"requested_mode,omitempty"`
	EffectiveMode  string    `json:"effective_mode,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	Dedupe         bool      `json:"dedupe"`
}

func (s *Service) libraryStorageType(ctx context.Context, libID uuid.UUID) string {
	if s.pool == nil || libID == uuid.Nil {
		return ""
	}
	var typ string
	_ = s.pool.QueryRow(ctx, `
		SELECT sp.type FROM libraries l
		JOIN storage_providers sp ON sp.id=l.storage_provider_id
		WHERE l.id=$1`, libID).Scan(&typ)
	return typ
}

func (s *Service) ResolveMigrateModes(ctx context.Context, requested string, sourceLib uuid.UUID) (req, effective, reason string) {
	req = strings.ToLower(strings.TrimSpace(requested))
	if req == "" {
		req = "copy"
	}
	srcType := s.libraryStorageType(ctx, sourceLib)
	if req == "move" && srcType == "managed" {
		return req, "move", "move_after_ingest"
	}
	if req == "move" {
		return req, "copy", "source_not_managed"
	}
	return req, "copy", ""
}

func (s *Service) MigrateHandler(getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) (err error) {
		var p MigratePayload
		if err = json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		src, _, prefix, e := getProv(ctx, p.Source)
		if e != nil {
			return e
		}
		srcType := s.libraryStorageType(ctx, p.Source)
		requested := strings.ToLower(strings.TrimSpace(p.Mode))
		if requested == "" {
			requested = "copy"
		}
		effectiveMove := requested == "move" && srcType == "managed"
		p.RequestedMode = requested
		if effectiveMove {
			p.EffectiveMode = "move"
			p.Reason = "move_after_ingest"
		} else {
			p.EffectiveMode = "copy"
			if requested == "move" && srcType != "managed" {
				p.Reason = "source_not_managed"
			}
		}
		it, e := src.List(ctx, prefix)
		if e != nil {
			return e
		}
		defer it.Close()
		var copied []string
		var srcKeys []string
		defer func() {
			if err == nil {
				return
			}
			for _, k := range copied {
				_ = s.managed.Delete(context.Background(), k)
			}
		}()
		for it.Next() {
			entry := it.Entry()
			if entry.IsDir {
				continue
			}
			rc, info, errOpen := src.Open(ctx, entry.Key)
			if errOpen != nil {
				continue
			}
			sz := entry.Size
			if info != nil {
				sz = info.Size
			}
			key := path.Join("migrated", entry.Key)
			if err = s.managed.Write(ctx, key, rc, storage.WriteInfo{Size: sz}); err != nil {
				rc.Close()
				return err
			}
			copied = append(copied, key)
			srcKeys = append(srcKeys, entry.Key)
			rc.Close()
		}
		dest, destID, _, e := getProv(ctx, p.Dest)
		if e != nil {
			err = e
			return err
		}
		err = s.scanner.ScanLibrary(ctx, destID, dest, "migrated", "migrate", job.ID)
		if err != nil {
			return err
		}
		if effectiveMove {
			for _, k := range srcKeys {
				var n int
				if s.pool != nil {
					_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM track_files WHERE storage_key=$1 AND deleted_at IS NULL`, k).Scan(&n)
				}
				if n == 0 {
					_ = src.Delete(ctx, k)
				}
			}
		}
		return nil
	}
}
