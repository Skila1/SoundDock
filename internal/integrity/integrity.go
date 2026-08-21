package integrity

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/scan"
	"github.com/sounddock/sounddock/internal/storage"
)

const JobName = "integrity.scan"

type Payload struct {
	LibraryID uuid.UUID `json:"library_id"`
}

type Report struct {
	LibraryID    uuid.UUID `json:"library_id"`
	FilesSeen    int       `json:"files_seen"`
	FilesMissing int       `json:"files_missing"`
	FilesOrphan  int       `json:"files_orphan"`
	FilesTrashed int       `json:"files_trashed"`
	Missing      []string  `json:"missing"`
	Orphans      []string  `json:"orphans"`
}

type Service struct {
	pool    *pgxpool.Pool
	getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)
}

func New(pool *pgxpool.Pool, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) *Service {
	return &Service{pool: pool, getProv: getProv}
}

func (s *Service) Handler() jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var p Payload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		_, err := s.Scan(ctx, p.LibraryID)
		return err
	}
}

func (s *Service) Scan(ctx context.Context, libID uuid.UUID) (Report, error) {
	rep, err := s.collect(ctx, libID)
	if err != nil {
		return rep, err
	}
	if s.pool != nil && libID != uuid.Nil {
		_, _ = s.pool.Exec(ctx, `INSERT INTO scan_runs (library_id, kind, files_seen, files_removed, finished_at) VALUES ($1,'integrity',$2,$3,now())`,
			libID, rep.FilesSeen, rep.FilesMissing)
	}
	return rep, nil
}

func (s *Service) collect(ctx context.Context, libID uuid.UUID) (Report, error) {
	rep := Report{LibraryID: libID, Missing: []string{}, Orphans: []string{}}
	if s.pool == nil || s.getProv == nil || libID == uuid.Nil {
		return rep, nil
	}
	prov, _, prefix, err := s.getProv(ctx, libID)
	if err != nil {
		return rep, err
	}
	known := map[string]struct{}{}
	rows, err := s.pool.Query(ctx, `
		SELECT storage_key FROM track_files
		WHERE library_id=$1 AND deleted_at IS NULL`, libID)
	if err != nil {
		return rep, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			continue
		}
		keys = append(keys, k)
		known[k] = struct{}{}
	}
	rows.Close()
	for _, k := range keys {
		if _, err := prov.Stat(ctx, k); err != nil {
			rep.FilesMissing++
			if len(rep.Missing) < 200 {
				rep.Missing = append(rep.Missing, k)
			}
		}
	}
	it, err := prov.List(ctx, prefix)
	if err != nil {
		return rep, err
	}
	defer it.Close()
	for it.Next() {
		e := it.Entry()
		if e.IsDir {
			continue
		}
		if scan.SkipScanKey(e.Key) {
			continue
		}
		if !scan.IsAudioExt(strings.ToLower(path.Ext(e.Key))) {
			continue
		}
		rep.FilesSeen++
		if _, ok := known[e.Key]; !ok {
			rep.FilesOrphan++
			if len(rep.Orphans) < 200 {
				rep.Orphans = append(rep.Orphans, e.Key)
			}
		}
	}
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM track_files WHERE library_id=$1 AND deleted_at IS NOT NULL`, libID).Scan(&rep.FilesTrashed)
	return rep, nil
}

func (s *Service) Orphans(ctx context.Context, libID uuid.UUID) ([]string, error) {
	rep, err := s.collect(ctx, libID)
	return rep.Orphans, err
}

func JobTypeOK(name string) bool {
	return strings.TrimSpace(name) == JobName
}

func MoveObject(ctx context.Context, prov storage.StorageProvider, from, to string) error {
	from = strings.TrimPrefix(strings.ReplaceAll(from, "\\", "/"), "/")
	to = strings.TrimPrefix(strings.ReplaceAll(to, "\\", "/"), "/")
	if from == "" || to == "" || from == to {
		return nil
	}
	rc, info, err := prov.Open(ctx, from)
	if err != nil {
		return err
	}
	var sz int64
	if info != nil {
		sz = info.Size
	}
	err = prov.Write(ctx, to, rc, storage.WriteInfo{Size: sz})
	rc.Close()
	if err != nil {
		return err
	}
	return prov.Delete(ctx, from)
}
