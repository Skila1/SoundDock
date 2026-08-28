package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

func canEditMeta(u *auth.User) bool {
	return u != nil && (u.IsAdmin || auth.HasPerm(u, "admin"))
}

func (s *Server) requireMetaEditor(w http.ResponseWriter, r *http.Request) bool {
	if canEditMeta(currentUser(r)) {
		return true
	}
	writeErr(w, 403, "forbidden", "admin or editor required")
	return false
}

func (s *Server) fieldLocked(ctx context.Context, entityType string, id uuid.UUID, field string) bool {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT 1 FROM metadata_locks WHERE entity_type=$1 AND entity_id=$2 AND field=$3`, entityType, id, field).Scan(&n)
	return err == nil
}

func (s *Server) trackGloballyLocked(ctx context.Context, id uuid.UUID) bool {
	var locked bool
	_ = s.Pool.QueryRow(ctx, `SELECT locked FROM tracks WHERE id=$1`, id).Scan(&locked)
	return locked
}

type trackMetaBody struct {
	Title       *string `json:"title"`
	Genre       *string `json:"genre"`
	Year        *int    `json:"year"`
	DiscNumber  *int    `json:"disc_number"`
	TrackNumber *int    `json:"track_number"`
	Explicit    *bool   `json:"explicit"`
	ISRC        *string `json:"isrc"`
	MBID        *string `json:"mbid"`
	Locked      *bool   `json:"locked"`
	Lyrics      *string `json:"lyrics"`
	Artist      *string `json:"artist"`
	WriteBack   bool    `json:"write_back"`
}

func (s *Server) getTrackMetadata(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	ctx := r.Context()
	u := currentUser(r)
	var (
		title, genre, isrc, mbid, album, source string
		codec, container                        *string
		dur, tn, dn                             int
		year                                    *int
		expl                                    *bool
		locked                                  bool
		albumID                                 *uuid.UUID
		libID                                   uuid.UUID
		gain, conf                              *float64
		bitDepth, sampleRate, bitrate, channels *int
		size                                    *int64
	)
	err = s.Pool.QueryRow(ctx, `
		SELECT t.title, t.duration_ms, t.track_number, t.disc_number, t.year, t.explicit, t.album_id, t.library_id,
		       t.genre_text, t.isrc, t.mbid, t.locked, t.manual_gain_db, t.metadata_source, t.metadata_confidence,
		       coalesce(al.title,''),
		       tf.codec, tf.bit_depth, tf.sample_rate, tf.bitrate, tf.channels, tf.size_bytes, tf.container
		FROM tracks t
		LEFT JOIN albums al ON al.id=t.album_id
		LEFT JOIN LATERAL (
			SELECT codec, bit_depth, sample_rate, bitrate, channels, size_bytes, container
			FROM track_files WHERE track_id=t.id AND quality='original' LIMIT 1
		) tf ON TRUE
		WHERE t.id=$1`, id).Scan(
		&title, &dur, &tn, &dn, &year, &expl, &albumID, &libID,
		&genre, &isrc, &mbid, &locked, &gain, &source, &conf,
		&album, &codec, &bitDepth, &sampleRate, &bitrate, &channels, &size, &container)
	if err != nil {
		writeErr(w, 404, "not_found", "track not found")
		return
	}
	var lyrics string
	var timed bool
	_ = s.Pool.QueryRow(ctx, `SELECT body, timed FROM lyrics WHERE track_id=$1 ORDER BY CASE WHEN source='user' THEN 0 ELSE 1 END LIMIT 1`, id).Scan(&lyrics, &timed)
	var playCount int
	var lastPlayed *string
	if u != nil {
		_ = s.Pool.QueryRow(ctx, `SELECT count, last_played_at::text FROM play_counts WHERE user_id=$1 AND track_id=$2`, u.ID, id).Scan(&playCount, &lastPlayed)
	}
	fav := false
	if u != nil {
		var n int
		_ = s.Pool.QueryRow(ctx, `SELECT 1 FROM favourites WHERE user_id=$1 AND entity_type='track' AND entity_id=$2`, u.ID, id).Scan(&n)
		fav = n == 1
	}
	org, ro := "", false
	_ = s.Pool.QueryRow(ctx, `SELECT organisation_mode, read_only FROM libraries WHERE id=$1`, libID).Scan(&org, &ro)
	locks := []string{}
	lrows, _ := s.Pool.Query(ctx, `SELECT field FROM metadata_locks WHERE entity_type='track' AND entity_id=$1`, id)
	if lrows != nil {
		defer lrows.Close()
		for lrows.Next() {
			var f string
			_ = lrows.Scan(&f)
			locks = append(locks, f)
		}
	}
	writeJSON(w, 200, map[string]any{
		"id": id, "title": title, "duration_ms": dur, "track_number": tn, "disc_number": dn,
		"year": year, "explicit": expl, "album_id": albumID, "library_id": libID, "album": album,
		"genre": genre, "isrc": isrc, "mbid": mbid, "locked": locked, "lyrics": lyrics, "lyrics_timed": timed,
		"artists": s.namedArtists(ctx, id),
		"codec":   codec, "container": container, "bit_depth": bitDepth, "sample_rate": sampleRate,
		"bitrate": bitrate, "channels": channels, "size_bytes": size,
		"manual_gain_db": gain, "metadata_source": source, "metadata_confidence": conf,
		"play_count": playCount, "last_played_at": lastPlayed, "favourite": fav,
		"organisation_mode": org, "read_only": ro, "locks": locks,
		"artwork_url":          "/api/v1/tracks/" + id.String() + "/artwork?size=page",
		"stream_url":           "/api/v1/tracks/" + id.String() + "/stream",
		"write_back_supported": org == "managed" && !ro,
	})
}

func (s *Server) patchTrackMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.requireMetaEditor(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	var body trackMetaBody
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if s.trackGloballyLocked(r.Context(), id) && body.Locked == nil {
		writeErr(w, 423, "locked", "track metadata is locked")
		return
	}
	updated := s.applyTrackMeta(r.Context(), id, body, currentUser(r).ID)
	out := map[string]any{"ok": true, "updated": updated, "write_back": body.WriteBack}
	if body.WriteBack {
		out["write_back_status"] = "pending_p3"
		out["write_back_path"] = "/api/v1/tracks/" + id.String() + "/writeback"
	}
	writeJSON(w, 200, out)
}

func (s *Server) applyTrackMeta(ctx context.Context, id uuid.UUID, body trackMetaBody, userID uuid.UUID) []string {
	var updated []string
	set := func(field, sql string, v any) {
		if s.fieldLocked(ctx, "track", id, field) {
			return
		}
		if _, err := s.Pool.Exec(ctx, sql, id, v); err == nil {
			updated = append(updated, field)
		}
	}
	if body.Title != nil {
		set("title", `UPDATE tracks SET title=$2, updated_at=now() WHERE id=$1`, *body.Title)
	}
	if body.Genre != nil {
		set("genre", `UPDATE tracks SET genre_text=$2, updated_at=now() WHERE id=$1`, *body.Genre)
	}
	if body.Year != nil {
		set("year", `UPDATE tracks SET year=$2, updated_at=now() WHERE id=$1`, *body.Year)
	}
	if body.DiscNumber != nil {
		set("disc_number", `UPDATE tracks SET disc_number=$2, updated_at=now() WHERE id=$1`, *body.DiscNumber)
	}
	if body.TrackNumber != nil {
		set("track_number", `UPDATE tracks SET track_number=$2, updated_at=now() WHERE id=$1`, *body.TrackNumber)
	}
	if body.Explicit != nil {
		set("explicit", `UPDATE tracks SET explicit=$2, updated_at=now() WHERE id=$1`, *body.Explicit)
	}
	if body.ISRC != nil {
		set("isrc", `UPDATE tracks SET isrc=$2, updated_at=now() WHERE id=$1`, *body.ISRC)
	}
	if body.MBID != nil {
		set("mbid", `UPDATE tracks SET mbid=$2, updated_at=now() WHERE id=$1`, *body.MBID)
	}
	if body.Locked != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE tracks SET locked=$2, updated_at=now() WHERE id=$1`, id, *body.Locked)
		updated = append(updated, "locked")
	}
	if body.Lyrics != nil && !s.fieldLocked(ctx, "track", id, "lyrics") {
		_, _ = s.Pool.Exec(ctx, `DELETE FROM lyrics WHERE track_id=$1 AND source='user'`, id)
		if strings.TrimSpace(*body.Lyrics) != "" {
			_, _ = s.Pool.Exec(ctx, `INSERT INTO lyrics (track_id, source, timed, body) VALUES ($1,'user',false,$2)`, id, *body.Lyrics)
		}
		updated = append(updated, "lyrics")
	}
	if body.Artist != nil && !s.fieldLocked(ctx, "track", id, "artist") {
		s.replaceTrackArtists(ctx, id, *body.Artist)
		updated = append(updated, "artist")
	}
	_ = userID
	return updated
}

func (s *Server) replaceTrackArtists(ctx context.Context, trackID uuid.UUID, names string) {
	_, _ = s.Pool.Exec(ctx, `DELETE FROM track_artists WHERE track_id=$1 AND role='primary'`, trackID)
	pos := 0
	for _, raw := range strings.Split(names, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		var aid uuid.UUID
		err := s.Pool.QueryRow(ctx, `SELECT id FROM artists WHERE lower(name)=lower($1) LIMIT 1`, name).Scan(&aid)
		if err != nil {
			_ = s.Pool.QueryRow(ctx, `INSERT INTO artists (name) VALUES ($1) RETURNING id`, name).Scan(&aid)
		}
		if aid != uuid.Nil {
			_, _ = s.Pool.Exec(ctx, `INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'primary',$3) ON CONFLICT DO NOTHING`, trackID, aid, pos)
			pos++
		}
	}
}

func (s *Server) bulkTrackMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.requireMetaEditor(w, r) {
		return
	}
	var body struct {
		IDs         []uuid.UUID `json:"ids"`
		Genre       *string     `json:"genre"`
		Year        *int        `json:"year"`
		Explicit    *bool       `json:"explicit"`
		DiscNumber  *int        `json:"disc_number"`
		TrackNumber *int        `json:"track_number"`
		WriteBack   bool        `json:"write_back"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	meta := trackMetaBody{Genre: body.Genre, Year: body.Year, Explicit: body.Explicit, DiscNumber: body.DiscNumber, TrackNumber: body.TrackNumber, WriteBack: body.WriteBack}
	if s.Jobs != nil && len(body.IDs) > 0 {
		jid, err := s.Jobs.Enqueue(r.Context(), "tracks.metadata", tracksMetaPayload{
			IDs: body.IDs, Genre: body.Genre, Year: body.Year, Explicit: body.Explicit,
			DiscNumber: body.DiscNumber, TrackNumber: body.TrackNumber, WriteBack: body.WriteBack,
			ActorID: currentUser(r).ID,
		})
		if err != nil {
			s.writeJobErr(w, err)
			return
		}
		out := map[string]any{"queued": true, "job_id": jid, "updated": len(body.IDs), "write_back": body.WriteBack}
		if body.WriteBack {
			out["write_back_status"] = "pending_p3"
			out["write_back_path"] = "/api/v1/tracks/bulk/writeback"
		}
		writeJSON(w, 202, out)
		return
	}
	n := 0
	for _, id := range body.IDs {
		if s.trackGloballyLocked(r.Context(), id) {
			continue
		}
		s.applyTrackMeta(r.Context(), id, meta, currentUser(r).ID)
		n++
	}
	out := map[string]any{"updated": n, "write_back": body.WriteBack}
	if body.WriteBack {
		out["write_back_status"] = "pending_p3"
		out["write_back_path"] = "/api/v1/tracks/bulk/writeback"
	}
	writeJSON(w, 200, out)
}

func (s *Server) putTrackLock(w http.ResponseWriter, r *http.Request) {
	if !s.requireMetaEditor(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	var body struct {
		Field  string `json:"field"`
		Locked bool   `json:"locked"`
	}
	_ = decodeJSON(r, &body)
	if body.Field == "" {
		writeErr(w, 400, "invalid", "field required")
		return
	}
	if body.Locked {
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO metadata_locks (entity_type, entity_id, field, locked_by)
			VALUES ('track',$1,$2,$3)
			ON CONFLICT (entity_type, entity_id, field) DO UPDATE SET locked_by=EXCLUDED.locked_by, locked_at=now()`,
			id, body.Field, currentUser(r).ID)
	} else {
		_, _ = s.Pool.Exec(r.Context(), `DELETE FROM metadata_locks WHERE entity_type='track' AND entity_id=$1 AND field=$2`, id, body.Field)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) patchAlbumMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.requireMetaEditor(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	var body struct {
		Title       *string `json:"title"`
		Year        *int    `json:"year"`
		Edition     *string `json:"edition_title"`
		Compilation *bool   `json:"is_compilation"`
		Label       *string `json:"label"`
		WriteBack   bool    `json:"write_back"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	ctx := r.Context()
	if body.Title != nil && !s.fieldLocked(ctx, "album", id, "title") {
		_, _ = s.Pool.Exec(ctx, `UPDATE albums SET title=$2 WHERE id=$1`, id, *body.Title)
	}
	if body.Year != nil && !s.fieldLocked(ctx, "album", id, "year") {
		_, _ = s.Pool.Exec(ctx, `UPDATE albums SET year=$2 WHERE id=$1`, id, *body.Year)
	}
	if body.Edition != nil && !s.fieldLocked(ctx, "album", id, "edition_title") {
		_, _ = s.Pool.Exec(ctx, `UPDATE albums SET edition_title=$2 WHERE id=$1`, id, *body.Edition)
	}
	if body.Compilation != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE albums SET is_compilation=$2 WHERE id=$1`, id, *body.Compilation)
	}
	if body.Label != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE albums SET label=$2 WHERE id=$1`, id, *body.Label)
	}
	out := map[string]any{"ok": true, "write_back": body.WriteBack}
	if body.WriteBack {
		out["write_back_status"] = "pending_p3"
		out["write_back_path"] = "/api/v1/albums/" + id.String() + "/writeback"
	}
	writeJSON(w, 200, out)
}

func (s *Server) patchArtistMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.requireMetaEditor(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	var body struct {
		Name     *string `json:"name"`
		SortName *string `json:"sort_name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if body.Name != nil && !s.fieldLocked(r.Context(), "artist", id, "name") {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE artists SET name=$2 WHERE id=$1`, id, *body.Name)
	}
	if body.SortName != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE artists SET sort_name=$2 WHERE id=$1`, id, *body.SortName)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) postTrackArtwork(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	var albumID *uuid.UUID
	if err := s.Pool.QueryRow(r.Context(), `SELECT album_id FROM tracks WHERE id=$1`, id).Scan(&albumID); err != nil {
		writeErr(w, 404, "not_found", "track not found")
		return
	}
	if albumID != nil {
		s.saveUploadedArtwork(w, r, "album", *albumID)
		return
	}
	s.saveUploadedArtwork(w, r, "track", id)
}

func (s *Server) postAlbumArtwork(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	s.saveUploadedArtwork(w, r, "album", id)
}

func (s *Server) postArtistArtwork(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	s.saveUploadedArtwork(w, r, "artist", id)
}

func (s *Server) saveUploadedArtwork(w http.ResponseWriter, r *http.Request, ownerType string, ownerID uuid.UUID) {
	if !s.requireMetaEditor(w, r) {
		return
	}
	if s.Art == nil {
		writeErr(w, 503, "artwork", "artwork store unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	var src io.Reader = r.Body
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(20 << 20); err != nil {
			writeErr(w, 400, "invalid", "image required")
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, 400, "invalid", "file field required")
			return
		}
		defer f.Close()
		src = f
	}
	aid, err := s.Art.Save(r.Context(), ownerType, ownerID, "user", src)
	if err != nil {
		writeErr(w, 400, "artwork", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "artwork_id": aid, "owner_type": ownerType, "owner_id": ownerID})
}
