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

// updateExistingTrackSQL skips globally locked tracks and leaves per-field
// metadata_locks untouched so a rescan cannot overwrite editor locks.
const updateExistingTrackSQL = `
	UPDATE tracks SET
	  title=CASE WHEN EXISTS (SELECT 1 FROM metadata_locks WHERE entity_type='track' AND entity_id=$1 AND field='title') THEN title ELSE $2 END,
	  album_id=CASE WHEN EXISTS (SELECT 1 FROM metadata_locks WHERE entity_type='track' AND entity_id=$1 AND field='album') THEN album_id ELSE $3 END,
	  disc_number=CASE WHEN EXISTS (SELECT 1 FROM metadata_locks WHERE entity_type='track' AND entity_id=$1 AND field='disc_number') THEN disc_number ELSE $4 END,
	  track_number=CASE WHEN EXISTS (SELECT 1 FROM metadata_locks WHERE entity_type='track' AND entity_id=$1 AND field='track_number') THEN track_number ELSE $5 END,
	  duration_ms=CASE WHEN $6 > 0 THEN $6 ELSE duration_ms END,
	  year=CASE WHEN EXISTS (SELECT 1 FROM metadata_locks WHERE entity_type='track' AND entity_id=$1 AND field='year') THEN year ELSE COALESCE(NULLIF($7,0), year) END,
	  genre_text=CASE WHEN EXISTS (SELECT 1 FROM metadata_locks WHERE entity_type='track' AND entity_id=$1 AND field='genre') THEN genre_text ELSE CASE WHEN $8 <> '' THEN $8 ELSE genre_text END END,
	  metadata_source=CASE WHEN $9 <> '' THEN $9 ELSE metadata_source END,
	  metadata_confidence=COALESCE($10, metadata_confidence),
	  mbid=CASE WHEN EXISTS (SELECT 1 FROM metadata_locks WHERE entity_type='track' AND entity_id=$1 AND field='mbid') THEN mbid ELSE CASE WHEN $11 <> '' THEN $11 ELSE mbid END END,
	  updated_at=now()
	WHERE id=$1 AND locked=false`

func (s *Scanner) trackOrFieldLocked(ctx context.Context, trackID uuid.UUID, field string) bool {
	if s == nil || s.pool == nil || trackID == uuid.Nil || field == "" {
		return false
	}
	var locked bool
	err := s.pool.QueryRow(ctx, `
		SELECT t.locked OR EXISTS (
			SELECT 1 FROM metadata_locks ml
			WHERE ml.entity_type='track' AND ml.entity_id=t.id AND ml.field=$2
		) FROM tracks t WHERE t.id=$1`, trackID, field).Scan(&locked)
	return err == nil && locked
}

func (s *Scanner) attachArtistCredits(ctx context.Context, trackID, fallbackArtist uuid.UUID, probe metadata.Probe) {
	if s == nil || s.pool == nil || trackID == uuid.Nil || s.trackOrFieldLocked(ctx, trackID, "artist") {
		return
	}
	credits := probe.Credits
	if len(credits) == 0 && fallbackArtist != uuid.Nil {
		_, _ = s.pool.Exec(ctx, `INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'primary',0) ON CONFLICT DO NOTHING`, trackID, fallbackArtist)
		return
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM track_artists WHERE track_id=$1 AND role IN ('primary','featured')`, trackID)
	for i, c := range credits {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		id, err := s.upsertArtistMeta(ctx, name, c.SortName, c.MBID)
		if err != nil || id == uuid.Nil {
			continue
		}
		role := c.Role
		if role == "" {
			if i == 0 {
				role = "primary"
			} else {
				role = "featured"
			}
		}
		_, _ = s.pool.Exec(ctx, `INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, trackID, id, role, i)
	}
}

func (s *Scanner) attachGenres(ctx context.Context, trackID uuid.UUID, probe metadata.Probe) {
	if s == nil || s.pool == nil || trackID == uuid.Nil || s.trackOrFieldLocked(ctx, trackID, "genre") {
		return
	}
	for _, name := range metadata.GenreList(probe) {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var gid uuid.UUID
		err := s.pool.QueryRow(ctx, `
			INSERT INTO genres (name) VALUES ($1)
			ON CONFLICT (name) DO UPDATE SET name=EXCLUDED.name
			RETURNING id`, name).Scan(&gid)
		if err != nil {
			continue
		}
		_, _ = s.pool.Exec(ctx, `INSERT INTO track_genres (track_id, genre_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, trackID, gid)
	}
}

func (s *Scanner) updateExistingTrack(ctx context.Context, trackID, albumID uuid.UUID, title string, probe metadata.Probe) {
	if s.pool == nil || trackID == uuid.Nil {
		return
	}
	title = s.keepStrongTitle(ctx, trackID, title)
	_, _ = s.pool.Exec(ctx, updateExistingTrackSQL,
		trackID, title, albumID, max1(probe.Disc), probe.Track, probe.DurationMS, probe.Year, probe.Genre, probe.Source, confOrNil(probe.Confidence), probe.MBID)
}

func (s *Scanner) keepStrongTitle(ctx context.Context, trackID uuid.UUID, incoming string) string {
	var existing, ref string
	_ = s.pool.QueryRow(ctx, `SELECT title, coalesce(acquisition_ref,'') FROM tracks WHERE id=$1`, trackID).Scan(&existing, &ref)
	if existing == "" {
		return incoming
	}
	if IsPlaceholderTitle(existing) {
		return incoming
	}
	if WeakIncomingTitle(incoming, ref) {
		return existing
	}
	return incoming
}

func WeakIncomingTitle(incoming, videoID string) bool {
	incoming = strings.TrimSpace(incoming)
	videoID = strings.TrimSpace(videoID)
	if incoming == "" || videoID == "" {
		return false
	}
	base := strings.TrimSuffix(path.Base(incoming), path.Ext(incoming))
	return strings.EqualFold(incoming, videoID) || strings.EqualFold(base, videoID)
}

func IsPlaceholderTitle(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || strings.EqualFold(s, "Restoring") || strings.HasPrefix(s, "YouTube ")
}

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
	if s.trackOrFieldLocked(ctx, trackID, "lyrics") {
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
		SELECT id, track_id FROM track_files
		WHERE content_hash=$1 AND deleted_at IS NULL`, hash)
	if err != nil {
		return
	}
	defer rows.Close()
	var fileIDs, trackIDs []uuid.UUID
	for rows.Next() {
		var fid, tid uuid.UUID
		if err := rows.Scan(&fid, &tid); err == nil {
			fileIDs = append(fileIDs, fid)
			trackIDs = append(trackIDs, tid)
		}
	}
	fileIDs = uniqueSortedUUIDs(fileIDs)
	if len(fileIDs) < DuplicateGroupMinMembers {
		return
	}
	s.upsertDuplicateGroup(ctx, dupMethodContentHash, contentHashBlockingKey(hash), fileIDs, uniqueSortedUUIDs(trackIDs))
}

// persistDuplicateGroups writes content-hash and artist+title groups (threshold ≥2).
// Cancellable: returns immediately if ctx is done or the maintenance-pool job was cancelled.
func (s *Scanner) persistDuplicateGroups(ctx context.Context, jobID uuid.UUID) {
	if s == nil || s.pool == nil {
		return
	}
	if s.jobCancelRequested(ctx, jobID) {
		return
	}
	s.persistContentHashReviewGroups(ctx)
	if s.jobCancelRequested(ctx, jobID) {
		return
	}
	s.persistArtistTitleGroups(ctx, jobID)
}

func (s *Scanner) jobCancelRequested(ctx context.Context, jobID uuid.UUID) bool {
	if ctx.Err() != nil {
		return true
	}
	if s == nil || s.pool == nil || jobID == uuid.Nil {
		return false
	}
	var cancel bool
	_ = s.pool.QueryRow(ctx, `SELECT cancel_requested FROM jobs WHERE id=$1`, jobID).Scan(&cancel)
	return cancel
}

func (s *Scanner) persistContentHashReviewGroups(ctx context.Context) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, array_agg(DISTINCT tf.track_id)
		FROM duplicate_groups g
		JOIN duplicates d ON d.group_id=g.id
		JOIN track_files tf ON tf.id=d.track_file_id
		WHERE tf.deleted_at IS NULL AND g.method=$1
		GROUP BY g.id
		HAVING count(DISTINCT tf.track_id) >= $2`, dupMethodContentHash, DuplicateGroupMinMembers)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var group uuid.UUID
		var tracks []uuid.UUID
		if err := rows.Scan(&group, &tracks); err != nil {
			continue
		}
		s.upsertReviewGroup(ctx, group, dupMethodContentHash, uniqueSortedUUIDs(tracks))
	}
}

func (s *Scanner) persistArtistTitleGroups(ctx context.Context, jobID uuid.UUID) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.title, t.duration_ms,
		  coalesce((
		    SELECT string_agg(ar.name, ' ' ORDER BY ta.position)
		    FROM track_artists ta
		    JOIN artists ar ON ar.id=ta.artist_id
		    WHERE ta.track_id=t.id AND ta.role='primary'
		  ), '') AS artist
		FROM tracks t`)
	if err != nil {
		return
	}
	defer rows.Close()
	type blockMem struct {
		tracks []timedTrack
	}
	blocks := map[string]*blockMem{}
	n := 0
	for rows.Next() {
		if n%200 == 199 && s.jobCancelRequested(ctx, jobID) {
			return
		}
		n++
		var id uuid.UUID
		var title, artist string
		var dur int
		if err := rows.Scan(&id, &title, &dur, &artist); err != nil {
			continue
		}
		if skipArtistTitleBlock(artist, title) {
			continue
		}
		key := ArtistTitleBlockingKey(artist, title)
		b := blocks[key]
		if b == nil {
			b = &blockMem{}
			blocks[key] = b
		}
		b.tracks = append(b.tracks, timedTrack{ID: id, DurationMS: dur})
	}
	for key, b := range blocks {
		if s.jobCancelRequested(ctx, jobID) {
			return
		}
		if len(b.tracks) < DuplicateGroupMinMembers {
			continue
		}
		clusters := ClusterByDuration(b.tracks, DurationWindowMS)
		for i, cluster := range clusters {
			if len(cluster) < DuplicateGroupMinMembers {
				continue
			}
			var trackIDs []uuid.UUID
			for _, t := range cluster {
				trackIDs = append(trackIDs, t.ID)
			}
			trackIDs = uniqueSortedUUIDs(trackIDs)
			fileIDs := s.trackFileIDs(ctx, trackIDs)
			s.upsertDuplicateGroup(ctx, dupMethodArtistTitle, artistTitleClusterKey(key, i), fileIDs, trackIDs)
		}
	}
}

func (s *Scanner) trackFileIDs(ctx context.Context, trackIDs []uuid.UUID) []uuid.UUID {
	if len(trackIDs) == 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM track_files WHERE track_id=ANY($1) AND deleted_at IS NULL`, trackIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return uniqueSortedUUIDs(ids)
}

func (s *Scanner) upsertDuplicateGroup(ctx context.Context, method, blockingKey string, fileIDs, trackIDs []uuid.UUID) {
	if len(fileIDs) < DuplicateGroupMinMembers && len(trackIDs) < DuplicateGroupMinMembers {
		return
	}
	group := s.lookupOrCreateGroup(ctx, method, blockingKey)
	if group == uuid.Nil {
		return
	}
	for _, id := range fileIDs {
		_, _ = s.pool.Exec(ctx, `INSERT INTO duplicates (group_id, track_file_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, group, id)
	}
	s.upsertReviewGroup(ctx, group, method, trackIDs)
}

func (s *Scanner) lookupOrCreateGroup(ctx context.Context, method, blockingKey string) uuid.UUID {
	var group uuid.UUID
	if blockingKey != "" {
		_ = s.pool.QueryRow(ctx, `SELECT id FROM duplicate_groups WHERE blocking_key=$1`, blockingKey).Scan(&group)
		if group != uuid.Nil {
			return group
		}
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO duplicate_groups (method, blocking_key) VALUES ($1, NULLIF($2,'')) RETURNING id`,
		method, blockingKey).Scan(&group)
	if err == nil && group != uuid.Nil {
		return group
	}
	if blockingKey != "" {
		_ = s.pool.QueryRow(ctx, `SELECT id FROM duplicate_groups WHERE blocking_key=$1`, blockingKey).Scan(&group)
		if group != uuid.Nil {
			return group
		}
	}
	if method == dupMethodContentHash && strings.HasPrefix(blockingKey, dupMethodContentHash+":") {
		hash := strings.TrimPrefix(blockingKey, dupMethodContentHash+":")
		_ = s.pool.QueryRow(ctx, `
			SELECT d.group_id FROM duplicates d
			JOIN track_files tf ON tf.id=d.track_file_id
			WHERE tf.content_hash=$1
			LIMIT 1`, hash).Scan(&group)
		if group != uuid.Nil {
			_, _ = s.pool.Exec(ctx, `UPDATE duplicate_groups SET blocking_key=$2 WHERE id=$1 AND (blocking_key IS NULL OR blocking_key='')`, group, blockingKey)
			return group
		}
	}
	_ = s.pool.QueryRow(ctx, `INSERT INTO duplicate_groups (method) VALUES ($1) RETURNING id`, method).Scan(&group)
	return group
}

func (s *Scanner) upsertReviewGroup(ctx context.Context, groupID uuid.UUID, reason string, trackIDs []uuid.UUID) {
	trackIDs = uniqueSortedUUIDs(trackIDs)
	if groupID == uuid.Nil || len(trackIDs) < DuplicateGroupMinMembers {
		return
	}
	var existing uuid.UUID
	var status string
	err := s.pool.QueryRow(ctx, `SELECT id, status FROM duplicate_review_groups WHERE group_id=$1`, groupID).Scan(&existing, &status)
	if err != nil || existing == uuid.Nil {
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO duplicate_review_groups (group_id, status, reason, track_ids)
			VALUES ($1, 'open', $2, $3)`, groupID, reason, trackIDs)
		return
	}
	if status != "open" {
		return
	}
	_, _ = s.pool.Exec(ctx, `
		UPDATE duplicate_review_groups SET reason=$2, track_ids=$3, updated_at=now() WHERE id=$1`,
		existing, reason, trackIDs)
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
