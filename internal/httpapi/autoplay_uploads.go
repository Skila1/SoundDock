package httpapi

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// Autoplay limits for songs a user uploaded themselves.
const (
	autoplayAlbumLimit    = 25
	autoplayUploaderLimit = 8
)

// autoplayUploadTracks applies the uploaded-music rule ahead of normal autoplay.
// When the seed is a file someone uploaded (not a YouTube download), the rest of
// its album plays next, in album order starting after the seed. With no album
// left, other songs the same person uploaded are used. Nil means the rule does
// not apply and normal autoplay should run.
func (s *Server) autoplayUploadTracks(ctx context.Context, userID, seed uuid.UUID, exclude []uuid.UUID) []uuid.UUID {
	if s == nil || s.Pool == nil || seed == uuid.Nil {
		return nil
	}
	var acquisition, key, hash, albumTitle string
	var albumID, seedLib uuid.UUID
	err := s.Pool.QueryRow(ctx, `
		SELECT t.acquisition, t.library_id,
			coalesce(t.album_id, '00000000-0000-0000-0000-000000000000'), coalesce(a.title, ''),
			tf.storage_key, coalesce(tf.content_hash, '')
		FROM tracks t
		JOIN track_files tf ON tf.track_id = t.id AND tf.quality = 'original' AND tf.deleted_at IS NULL
		LEFT JOIN albums a ON a.id = t.album_id
		WHERE t.id = $1
		LIMIT 1`, seed).Scan(&acquisition, &seedLib, &albumID, &albumTitle, &key, &hash)
	if err != nil || !isUploadedFile(acquisition, key) {
		return nil
	}
	libs := s.autoplayLibs(ctx, userID, seedLib)
	if len(libs) == 0 {
		return nil
	}
	if exclude == nil {
		exclude = []uuid.UUID{}
	}
	if albumID != uuid.Nil && !isPlaceholderAlbum(albumTitle) {
		if ids := s.autoplayAlbumTracks(ctx, seed, albumID, libs, exclude); len(ids) > 0 {
			return ids
		}
	}
	uploader := s.trackUploader(ctx, key, hash)
	if uploader == uuid.Nil {
		return nil
	}
	return s.autoplayUploaderTracks(ctx, uploader, libs, append(append([]uuid.UUID{}, exclude...), seed))
}

// isUploadedFile reports whether a track file came from a user upload. Uploads
// land under "<prefix>/uploads/..."; YouTube downloads are tagged by acquisition.
func isUploadedFile(acquisition, key string) bool {
	switch strings.ToLower(strings.TrimSpace(acquisition)) {
	case "youtube", "scapex", "url":
		return false
	}
	key = strings.TrimPrefix(key, "/")
	return strings.HasPrefix(key, "uploads/") || strings.Contains(key, "/uploads/")
}

// isPlaceholderAlbum is true for the album the scanner invents for untagged files.
func isPlaceholderAlbum(title string) bool {
	t := strings.TrimSpace(title)
	return t == "" || strings.EqualFold(t, "Unknown Album")
}

// autoplayLibs is the set of libraries autoplay may draw from: the listener's
// readable libraries, or the seed's library for sessions without a user (Discord).
func (s *Server) autoplayLibs(ctx context.Context, userID, seedLib uuid.UUID) []uuid.UUID {
	libs := s.libraryIDs(ctx, s.autoplayUser(ctx, userID))
	if len(libs) == 0 && seedLib != uuid.Nil {
		libs = []uuid.UUID{seedLib}
	}
	return libs
}

// autoplayAlbumTracks returns the album's other playable tracks, the ones after
// the seed first, then any before it.
func (s *Server) autoplayAlbumTracks(ctx context.Context, seed, albumID uuid.UUID, libs, exclude []uuid.UUID) []uuid.UUID {
	rows, err := s.Pool.Query(ctx, `
		WITH me AS (SELECT disc_number, track_number FROM tracks WHERE id = $1)
		SELECT t.id
		FROM tracks t, me
		WHERE t.album_id = $2
		  AND t.id <> $1
		  AND t.library_id = ANY($3)
		  AND t.id <> ALL($4)
		  AND EXISTS (SELECT 1 FROM track_files tf WHERE tf.track_id = t.id AND tf.quality = 'original' AND tf.deleted_at IS NULL)
		ORDER BY ((t.disc_number, t.track_number) <= (me.disc_number, me.track_number)),
			t.disc_number, t.track_number, t.title
		LIMIT $5`, seed, albumID, libs, exclude, autoplayAlbumLimit)
	if err != nil {
		return nil
	}
	return collectUUIDs(rows)
}

// trackUploader finds who uploaded a file: by content hash for single uploads,
// or by the upload session folder for zip uploads.
func (s *Server) trackUploader(ctx context.Context, key, hash string) uuid.UUID {
	var uid uuid.UUID
	if hash != "" {
		if err := s.Pool.QueryRow(ctx, `
			SELECT user_id FROM upload_sessions
			WHERE content_hash = $1 AND state = 'complete'
			ORDER BY updated_at LIMIT 1`, hash).Scan(&uid); err == nil {
			return uid
		}
	}
	if sess := zipUploadSession(key); sess != uuid.Nil {
		if err := s.Pool.QueryRow(ctx, `SELECT user_id FROM upload_sessions WHERE id = $1`, sess).Scan(&uid); err == nil {
			return uid
		}
	}
	return uuid.Nil
}

// zipUploadSession extracts the session ID from "<prefix>/uploads/zip/<session>/...".
func zipUploadSession(key string) uuid.UUID {
	i := strings.Index(key, "uploads/zip/")
	if i < 0 {
		return uuid.Nil
	}
	rest := key[i+len("uploads/zip/"):]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		rest = rest[:j]
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// autoplayUploaderTracks picks random playable songs the given user uploaded.
func (s *Server) autoplayUploaderTracks(ctx context.Context, uploader uuid.UUID, libs, exclude []uuid.UUID) []uuid.UUID {
	rows, err := s.Pool.Query(ctx, `
		WITH up AS (SELECT id, content_hash FROM upload_sessions WHERE user_id = $1)
		SELECT t.id
		FROM tracks t
		JOIN track_files tf ON tf.track_id = t.id AND tf.quality = 'original' AND tf.deleted_at IS NULL
		WHERE t.library_id = ANY($2)
		  AND t.id <> ALL($3)
		  AND t.acquisition NOT IN ('youtube', 'scapex', 'url')
		  AND (tf.content_hash IN (SELECT content_hash FROM up WHERE content_hash IS NOT NULL)
		       OR substring(tf.storage_key from 'uploads/zip/([0-9a-fA-F-]{36})/') IN (SELECT id::text FROM up))
		GROUP BY t.id
		ORDER BY random()
		LIMIT $4`, uploader, libs, exclude, autoplayUploaderLimit)
	if err != nil {
		return nil
	}
	return collectUUIDs(rows)
}

type uuidRows interface {
	Next() bool
	Scan(...any) error
	Close()
}

func collectUUIDs(rows uuidRows) []uuid.UUID {
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil && id != uuid.Nil {
			out = append(out, id)
		}
	}
	return out
}
