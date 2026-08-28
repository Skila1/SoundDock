package scapex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	JobTypeReplaceSource = "tracks.replace_source"
	JobTypeFetch         = "scapex.fetch"
	QualityReplaced      = "replaced"
)

// ReplaceOpts is the offline acquire path for replacing an existing track's media.
type ReplaceOpts struct {
	JobID       uuid.UUID
	TrackID     uuid.UUID
	URLs        []string
	DestLibrary uuid.UUID
	Policy      string
	UserID      uuid.UUID
	Quota       QuotaFunc
}

type RetiredFile struct {
	LibraryID  uuid.UUID
	StorageKey string
}

type CommitReplaceInput struct {
	TrackID     uuid.UUID
	LibraryID   uuid.UUID
	StorageKey  string
	SizeBytes   int64
	ContentHash string
	Codec       string
	Container   string
	DurationMS  int
	Provider    string
	SourceRef   string
	JobID       uuid.UUID
}

// ReplaceCoalesceKey reuses Wave 6 dest+policy fields and scopes by track
// so two catalogue rows cannot share one replace job.
func ReplaceCoalesceKey(trackID uuid.UUID, provider, sourceRef, destLibraryID, mediaPolicyID string) string {
	return strings.Join([]string{
		"replace",
		trackID.String(),
		CoalesceKey(provider, sourceRef, destLibraryID, mediaPolicyID),
	}, "|")
}

func ReplaceStorageKey(trackID, jobID uuid.UUID, ext string) string {
	if !strings.HasPrefix(ext, ".") {
		if ext == "" {
			ext = ".m4a"
		} else {
			ext = "." + ext
		}
	}
	id := jobID
	if id == uuid.Nil {
		id = uuid.New()
	}
	return filepath.ToSlash(filepath.Join("replace", trackID.String(), id.String()+strings.ToLower(ext)))
}

func CodecFromExt(ext string) (codec, container string) {
	switch strings.ToLower(ext) {
	case ".m4a", ".aac":
		return "aac", "mp4"
	case ".mp3":
		return "mp3", "mpeg"
	case ".opus":
		return "opus", "ogg"
	case ".ogg", ".oga":
		return "vorbis", "ogg"
	case ".flac":
		return "flac", "flac"
	case ".wav":
		return "pcm", "wav"
	default:
		e := strings.TrimPrefix(strings.ToLower(ext), ".")
		if e == "" {
			return "aac", "mp4"
		}
		return e, e
	}
}

func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// AcquireReplace downloads allowlisted YouTube audio into a job-scoped work
// dir. It does not scan inbox, wait on tracks, or apply playback intents.
func (s *Service) AcquireReplace(ctx context.Context, opts ReplaceOpts) ([]LocalTrack, error) {
	if s == nil || s.dock == nil {
		return nil, fmt.Errorf("SoundDock volume/database is not attached")
	}
	jobID := opts.JobID
	if jobID == uuid.Nil {
		jobID = uuid.New()
	}
	policy := NormalizePolicy(opts.Policy)
	work := s.dock.JobWork(jobID)
	if err := os.MkdirAll(work, 0o775); err != nil {
		return nil, err
	}

	var locals []LocalTrack
	for _, raw := range opts.URLs {
		src := WatchURL(raw)
		if src == "" || VideoID(src) == "" {
			os.RemoveAll(work)
			return nil, fmt.Errorf("not an allowlisted YouTube watch URL or video id")
		}
		got, err := s.download(ctx, src, work, policy)
		if err != nil {
			os.RemoveAll(work)
			return nil, err
		}
		locals = append(locals, got...)
	}
	if len(locals) == 0 {
		os.RemoveAll(work)
		return nil, fmt.Errorf("yt-dlp produced no audio")
	}

	var extra int64
	for _, loc := range locals {
		if st, err := os.Stat(loc.Path); err == nil {
			extra += st.Size()
		}
	}
	if opts.Quota != nil {
		lib := opts.DestLibrary
		if err := opts.Quota(ctx, opts.UserID, lib, extra); err != nil {
			os.RemoveAll(work)
			return nil, err
		}
	}
	return locals, nil
}

// CommitReplace inserts a new original track_files row and track_sources.
// Old originals keep their storage_key; they are retired (deleted_at + quality
// replaced) so later GET /stream and Discord opens see the new file. Bytes at
// the old key are not truncated.
func CommitReplace(ctx context.Context, pool *pgxpool.Pool, in CommitReplaceInput) ([]RetiredFile, error) {
	if pool == nil {
		return nil, fmt.Errorf("no database")
	}
	if in.TrackID == uuid.Nil || in.LibraryID == uuid.Nil || strings.TrimSpace(in.StorageKey) == "" {
		return nil, fmt.Errorf("track, library, and storage_key required")
	}
	ref := CanonicalSourceRef(in.SourceRef)
	provider := NormalizeProvider(in.Provider)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT library_id, storage_key FROM track_files
		WHERE track_id=$1 AND quality='original' AND deleted_at IS NULL`, in.TrackID)
	if err != nil {
		return nil, err
	}
	var retired []RetiredFile
	for rows.Next() {
		var f RetiredFile
		if err := rows.Scan(&f.LibraryID, &f.StorageKey); err != nil {
			rows.Close()
			return nil, err
		}
		if f.StorageKey != "" && f.StorageKey != in.StorageKey {
			retired = append(retired, f)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, content_hash, codec, container, quality)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,'original')`,
		in.TrackID, in.LibraryID, in.StorageKey, in.SizeBytes, in.ContentHash, in.Codec, in.Container); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE track_files
		SET deleted_at=now(), quality=$3
		WHERE track_id=$1 AND quality='original' AND deleted_at IS NULL AND storage_key<>$2`,
		in.TrackID, in.StorageKey, QualityReplaced); err != nil {
		return nil, err
	}

	var job any
	if in.JobID != uuid.Nil {
		job = in.JobID
	}
	if ref != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO track_sources (track_id, provider, source_ref, job_id)
			VALUES ($1,$2,$3,$4)`, in.TrackID, provider, ref, job); err != nil {
			return nil, err
		}
	}

	dur := in.DurationMS
	if _, err := tx.Exec(ctx, `
		UPDATE tracks SET
		  acquisition=CASE WHEN $2 <> '' THEN $2 ELSE acquisition END,
		  acquisition_ref=CASE WHEN $3 <> '' THEN $3 ELSE acquisition_ref END,
		  media_unavailable_at=NULL,
		  duration_ms=CASE WHEN $4 > 0 THEN $4 ELSE duration_ms END,
		  updated_at=now()
		WHERE id=$1`, in.TrackID, provider, ref, dur); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return retired, nil
}

// ReplaceMediaBusy is true when any session still has this track current, or an
// active scapex.fetch / tracks.replace_source job (other than exceptJob) is
// bound to it. Callers must not NAS-delete; skip physical delete while busy.
func ReplaceMediaBusy(ctx context.Context, pool *pgxpool.Pool, trackID, exceptJob uuid.UUID) (bool, error) {
	if pool == nil || trackID == uuid.Nil {
		return true, nil
	}
	var busy bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM playback_sessions WHERE current_track_id=$1
		)`, trackID).Scan(&busy)
	if err != nil {
		return true, err
	}
	if busy {
		return true, nil
	}
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM jobs j
			WHERE j.status IN ('queued','running','retry')
			  AND j.id IS DISTINCT FROM $2
			  AND (
			    (j.type=$3 AND j.payload->>'track_id'=$1)
			    OR (
			      j.type=$4 AND (
			        j.payload->>'track_id'=$1
			        OR EXISTS (
			          SELECT 1 FROM acquisition_intents i
			          WHERE i.job_id=j.id AND i.track_id=$5
			        )
			      )
			    )
			  )
		)`, trackID.String(), exceptJob, JobTypeReplaceSource, JobTypeFetch, trackID).Scan(&busy)
	if err != nil {
		return true, err
	}
	return busy, nil
}
