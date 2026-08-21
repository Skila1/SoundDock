package scan

import (
	"context"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/metadata"
	"github.com/sounddock/sounddock/internal/organise"
	"github.com/sounddock/sounddock/internal/storage"
)

func confOrNil(c float64) any {
	if c <= 0 {
		return nil
	}
	return c
}

func origSizePtr(orig, stored int64) any {
	if orig <= 0 || orig == stored {
		return nil
	}
	return orig
}

func (s *Scanner) insertLyrics(ctx context.Context, trackID uuid.UUID, probe metadata.Probe) {
	body := strings.TrimSpace(probe.Lyrics)
	if body == "" || s.pool == nil {
		return
	}
	var exists bool
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lyrics WHERE track_id=$1 AND source='embedded')`, trackID).Scan(&exists)
	if exists {
		_, _ = s.pool.Exec(ctx, `UPDATE lyrics SET body=$2, timed=$3 WHERE track_id=$1 AND source='embedded'`, trackID, body, metadata.LyricsTimed(body))
		return
	}
	_, _ = s.pool.Exec(ctx, `INSERT INTO lyrics (track_id, source, timed, body) VALUES ($1,'embedded',$2,$3)`, trackID, metadata.LyricsTimed(body), body)
}

func (s *Scanner) writeDuplicateGroup(ctx context.Context, fileID uuid.UUID, hash string) {
	if s.pool == nil || hash == "" || fileID == uuid.Nil {
		return
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM track_files
		WHERE content_hash=$1 AND id<>$2 AND deleted_at IS NULL`, hash, fileID)
	if err != nil {
		return
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	var group uuid.UUID
	_ = s.pool.QueryRow(ctx, `
		SELECT d.group_id FROM duplicates d
		JOIN track_files tf ON tf.id=d.track_file_id
		WHERE tf.content_hash=$1
		LIMIT 1`, hash).Scan(&group)
	if group == uuid.Nil {
		if err := s.pool.QueryRow(ctx, `INSERT INTO duplicate_groups (method) VALUES ('content_hash') RETURNING id`).Scan(&group); err != nil {
			return
		}
	}
	ids = append(ids, fileID)
	for _, id := range ids {
		_, _ = s.pool.Exec(ctx, `INSERT INTO duplicates (group_id, track_file_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, group, id)
	}
}

func (s *Scanner) maybeReorganise(ctx context.Context, libID uuid.UUID, prov storage.StorageProvider, fileID uuid.UUID, e *storage.Entry, probe metadata.Probe, tmpl string) {
	if e == nil || !prov.Capabilities().Write {
		return
	}
	dest := organise.Apply(tmpl, organise.Vars{
		AlbumArtist: probe.AlbumArtist,
		Artist:      probe.Artist,
		Album:       probe.Album,
		Year:        probe.Year,
		Disc:        probe.Disc,
		DiscCount:   probe.DiscTotal,
		Track:       probe.Track,
		Title:       probe.Title,
		Ext:         path.Ext(e.Key),
	})
	dest = strings.TrimPrefix(strings.ReplaceAll(dest, "\\", "/"), "/")
	if dest == "" || dest == e.Key {
		return
	}
	var taken bool
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM track_files WHERE library_id=$1 AND storage_key=$2 AND id<>$3)`, libID, dest, fileID).Scan(&taken)
	if taken {
		return
	}
	if err := moveObject(ctx, prov, e.Key, dest); err != nil {
		return
	}
	_, _ = s.pool.Exec(ctx, `UPDATE track_files SET storage_key=$2 WHERE id=$1`, fileID, dest)
	e.Key = dest
}

func moveObject(ctx context.Context, prov storage.StorageProvider, from, to string) error {
	if from == to {
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
