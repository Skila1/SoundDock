package scapex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Dock is how ScapeX joins SoundDock: shared Postgres + the managed volume.
// It does not call SoundDock's HTTP API.
type Dock struct {
	pool    *pgxpool.Pool
	inbox   string
	ownPool bool
}

func NewDockWithPool(pool *pgxpool.Pool, inbox string) *Dock {
	if inbox == "" {
		inbox = "/data/managed/inbox"
	}
	_ = os.MkdirAll(inbox, 0o775)
	return &Dock{pool: pool, inbox: inbox}
}

func NewDock(ctx context.Context, databaseURL, inbox string) (*Dock, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("SD_DATABASE_URL is required")
	}
	if inbox == "" {
		inbox = "/data/managed/inbox"
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := os.MkdirAll(inbox, 0o775); err != nil {
		pool.Close()
		return nil, err
	}
	return &Dock{pool: pool, inbox: inbox, ownPool: true}, nil
}

func (d *Dock) Close() {
	if d != nil && d.ownPool && d.pool != nil {
		d.pool.Close()
	}
}

func (d *Dock) Inbox() string { return d.inbox }

func (d *Dock) LibraryID(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := d.pool.QueryRow(ctx, `
		SELECT l.id
		FROM libraries l
		JOIN storage_providers sp ON sp.id = l.storage_provider_id
		WHERE l.read_only = false AND sp.type IN ('managed', 'local')
		ORDER BY CASE WHEN l.is_default THEN 0 ELSE 1 END, CASE WHEN lower(l.name) = 'music' THEN 0 ELSE 1 END, l.created_at
		LIMIT 1`).Scan(&id)
	return id, err
}

func (d *Dock) EnqueueInboxScan(ctx context.Context, lib uuid.UUID) error {
	payload, err := json.Marshal(map[string]any{
		"library_id": lib.String(),
		"kind":       "inbox",
		"prefix":     "inbox",
	})
	if err != nil {
		return err
	}
	var existing uuid.UUID
	err = d.pool.QueryRow(ctx, `
		SELECT id FROM jobs
		WHERE type='library.scan' AND payload=$1::jsonb AND status IN ('queued','running','retry')
		LIMIT 1`, payload).Scan(&existing)
	if err == nil {
		return nil
	}
	_, err = d.pool.Exec(ctx, `INSERT INTO jobs (type, payload) VALUES ('library.scan', $1)`, payload)
	return err
}

func (d *Dock) WaitTrack(ctx context.Context, lib uuid.UUID, videoID, title string) (uuid.UUID, error) {
	needle := strings.TrimSpace(videoID)
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	for {
		id, ok, err := d.findTrack(ctx, lib, needle)
		if err != nil {
			return uuid.Nil, err
		}
		if ok {
			return id, nil
		}
		select {
		case <-ctx.Done():
			return uuid.Nil, fmt.Errorf("timed out waiting for SoundDock to index %s", needle)
		case <-t.C:
		}
	}
}

func (d *Dock) findTrack(ctx context.Context, lib uuid.UUID, videoID string) (uuid.UUID, bool, error) {
	if videoID == "" || d == nil || d.pool == nil {
		return uuid.Nil, false, nil
	}
	var id uuid.UUID
	err := d.pool.QueryRow(ctx, `
		SELECT t.id FROM tracks t
		JOIN track_files tf ON tf.track_id = t.id AND tf.deleted_at IS NULL
		WHERE t.library_id=$1 AND t.acquisition_ref = $2
		ORDER BY t.created_at DESC LIMIT 1`, lib, videoID).Scan(&id)
	if err != nil {
		return uuid.Nil, false, nil
	}
	d.tagAcquisition(ctx, id, videoID)
	return id, true, nil
}

func (d *Dock) HasOriginal(ctx context.Context, lib uuid.UUID, videoID string) bool {
	if d == nil || d.pool == nil || videoID == "" {
		return false
	}
	var ok bool
	_ = d.pool.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM tracks t
		  JOIN track_files tf ON tf.track_id=t.id AND tf.quality='original' AND tf.deleted_at IS NULL
		  WHERE t.library_id=$1 AND t.acquisition_ref=$2
		)`, lib, videoID).Scan(&ok)
	return ok
}

func (d *Dock) ReadyOriginals(ctx context.Context, jobID, lib uuid.UUID) ([]uuid.UUID, bool) {
	if d == nil || d.pool == nil || jobID == uuid.Nil {
		return nil, false
	}
	intents, err := ListJobIntents(ctx, d.pool, jobID)
	if err != nil || len(intents) == 0 {
		return nil, false
	}
	var ids []uuid.UUID
	for _, in := range intents {
		if in.TrackID == uuid.Nil {
			return nil, false
		}
		var files int
		_ = d.pool.QueryRow(ctx, `
			SELECT count(*) FROM track_files
			WHERE track_id=$1 AND quality='original' AND deleted_at IS NULL`, in.TrackID).Scan(&files)
		if files == 0 {
			return nil, false
		}
		ids = append(ids, in.TrackID)
	}
	return ids, len(ids) > 0
}

// AttachLocals writes job-scoped inbox files onto the existing stub for each video id.
func (d *Dock) AttachLocals(ctx context.Context, lib, jobID uuid.UUID, locals []LocalTrack) ([]uuid.UUID, error) {
	if d == nil || d.pool == nil {
		return nil, fmt.Errorf("no database")
	}
	var ids []uuid.UUID
	for _, loc := range locals {
		vid := VideoID(loc.VideoID)
		if vid == "" {
			vid = VideoID(strings.TrimSuffix(filepath.Base(loc.Path), filepath.Ext(loc.Path)))
		}
		if vid == "" {
			return ids, fmt.Errorf("job ingest missing video id")
		}
		id, err := d.ensureStubForAttach(ctx, lib, vid, loc)
		if err != nil {
			return ids, err
		}
		key := path.Join("inbox", "jobs", jobID.String(), filepath.Base(loc.Path))
		var size int64
		if st, err := os.Stat(loc.Path); err == nil {
			size = st.Size()
		}
		_, err = d.pool.Exec(ctx, `
			INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, quality, codec, container)
			VALUES ($1,$2,$3,$4,'original','','')
			ON CONFLICT (library_id, storage_key) DO UPDATE SET
			  track_id=EXCLUDED.track_id, size_bytes=EXCLUDED.size_bytes, deleted_at=NULL`,
			id, lib, key, size)
		if err != nil {
			return ids, err
		}
		d.tagAcquisition(ctx, id, vid)
		if !IsWeakTaglessTitle(loc.Title, vid) {
			ApplyStubHint(ctx, d.pool, id, TrackHint{Title: loc.Title, Artist: loc.Artist, DurationMS: loc.DurationMS})
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (d *Dock) ensureStubForAttach(ctx context.Context, lib uuid.UUID, vid string, loc LocalTrack) (uuid.UUID, error) {
	var id uuid.UUID
	err := d.pool.QueryRow(ctx, `
		SELECT id FROM tracks WHERE library_id=$1 AND acquisition_ref=$2
		ORDER BY created_at DESC LIMIT 1`, lib, vid).Scan(&id)
	if err == nil {
		return id, nil
	}
	hint := TrackHint{Title: loc.Title, Artist: loc.Artist, DurationMS: loc.DurationMS}
	if IsWeakTaglessTitle(loc.Title, vid) {
		hint.Title = ""
	}
	return EnsureStubTrack(ctx, d.pool, lib, vid, hint)
}

func (d *Dock) tagAcquisition(ctx context.Context, id uuid.UUID, videoID string) {
	if id == uuid.Nil {
		return
	}
	_, _ = d.pool.Exec(ctx, `
		UPDATE tracks SET
		  acquisition=CASE WHEN acquisition='' THEN 'youtube' ELSE acquisition END,
		  acquisition_ref=CASE WHEN $2 <> '' AND acquisition_ref='' THEN $2 ELSE acquisition_ref END,
		  media_unavailable_at=NULL,
		  updated_at=now()
		WHERE id=$1`, id, strings.TrimSpace(videoID))
}

// FinalizeDownload keeps job-scoped paths. It must not copy to inbox/{videoID}.ext.
func (d *Dock) FinalizeDownload(src LocalTrack) (LocalTrack, error) {
	if src.VideoID == "" {
		src.VideoID = strings.TrimSuffix(filepath.Base(src.Path), filepath.Ext(src.Path))
	}
	return src, nil
}
