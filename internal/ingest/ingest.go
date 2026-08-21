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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/external"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/scan"
	"github.com/sounddock/sounddock/internal/ssrf"
	"github.com/sounddock/sounddock/internal/storage"
)

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
	LibraryID uuid.UUID `json:"library_id"`
}

func (s *Service) URLHandler(getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var p URLPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		if external.IsPlaylistURL(p.URL) {
			return fmt.Errorf("playlist URLs belong in External Playlist Import, not Remote Import")
		}
		opt := ssrf.DefaultOptions()
		opt.MaxBytes = s.maxBytes
		rc, _, _, err := ssrf.Fetch(ctx, p.URL, opt)
		if err != nil {
			return err
		}
		defer rc.Close()
		tmp, err := os.CreateTemp(s.staging, "url-*")
		if err != nil {
			return err
		}
		hw := sha256.New()
		n, err := io.Copy(tmp, io.TeeReader(rc, hw))
		name := tmp.Name()
		tmp.Close()
		if err != nil {
			os.Remove(name)
			return err
		}
		hash := hex.EncodeToString(hw.Sum(nil))
		var existing string
		if err := s.pool.QueryRow(ctx, `SELECT storage_key FROM track_files WHERE content_hash=$1 LIMIT 1`, hash).Scan(&existing); err == nil && existing != "" {
			os.Remove(name)
			return nil
		}
		ext := path.Ext(p.URL)
		if ext == "" {
			ext = ".bin"
		}
		key := path.Join("imports", hash[:2], hash+ext)
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		err = s.managed.Write(ctx, key, f, storage.WriteInfo{Size: n})
		f.Close()
		os.Remove(name)
		if err != nil {
			return err
		}
		prov, libID, _, err := getProv(ctx, p.LibraryID)
		if err != nil {
			return err
		}
		return s.scanner.ScanLibrary(ctx, libID, prov, "imports/"+hash[:2], "import", job.ID)
	}
}

type UploadComplete struct {
	SessionID uuid.UUID `json:"session_id"`
}

func (s *Service) CreateUpload(ctx context.Context, user, lib uuid.UUID, filename string, size int64) (uuid.UUID, string, error) {
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

func (s *Service) FinishUpload(ctx context.Context, id uuid.UUID, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) error {
	var staging, filename string
	var lib uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT staging_key, filename, library_id FROM upload_sessions WHERE id=$1`, id).Scan(&staging, &filename, &lib); err != nil {
		return err
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
	key := path.Join("uploads", hash[:2], hash+path.Ext(filename))
	st, _ := f.Stat()
	err = s.managed.Write(ctx, key, f, storage.WriteInfo{Size: st.Size()})
	f.Close()
	if err != nil {
		return err
	}
	os.Remove(staging)
	_, _ = s.pool.Exec(ctx, `UPDATE upload_sessions SET state='complete', content_hash=$2 WHERE id=$1`, id, hash)
	prov, libID, _, err := getProv(ctx, lib)
	if err != nil {
		return err
	}
	return s.scanner.ScanLibrary(ctx, libID, prov, "uploads/"+hash[:2], "upload", uuid.Nil)
}

type MigratePayload struct {
	Source uuid.UUID `json:"source_library_id"`
	Dest   uuid.UUID `json:"dest_library_id"`
	Mode   string    `json:"mode"` // copy|move
	Dedupe bool      `json:"dedupe"`
}

func (s *Service) MigrateHandler(getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var p MigratePayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		src, _, prefix, err := getProv(ctx, p.Source)
		if err != nil {
			return err
		}
		it, err := src.List(ctx, prefix)
		if err != nil {
			return err
		}
		defer it.Close()
		for it.Next() {
			e := it.Entry()
			if e.IsDir {
				continue
			}
			rc, info, err := src.Open(ctx, e.Key)
			if err != nil {
				continue
			}
			sz := e.Size
			if info != nil {
				sz = info.Size
			}
			_ = s.managed.Write(ctx, path.Join("migrated", e.Key), rc, storage.WriteInfo{Size: sz})
			rc.Close()
			if p.Mode == "move" {
				_ = src.Delete(ctx, e.Key)
			}
		}
		dest, destID, _, err := getProv(ctx, p.Dest)
		if err != nil {
			return err
		}
		return s.scanner.ScanLibrary(ctx, destID, dest, "migrated", "migrate", job.ID)
	}
}
