package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	KeepForever *bool   `json:"keep_forever"`
	AlbumID     *string `json:"album_id"`
	// Album is an album title; it links the track to a matching album in the
	// same library, or creates one. Ignored when AlbumID is also sent.
	Album        *string  `json:"album"`
	ManualGainDB *float64 `json:"manual_gain_db"`
	WriteBack    bool     `json:"write_back"`
}

func (s *Server) getTrackMetadata(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	if !s.requireTrackLibrary(w, r, id, "read") {
		return
	}
	ctx := r.Context()
	u := currentUser(r)
	var (
		title, genre, album, source, acq        string
		isrc, mbid                              *string
		codec, container                        *string
		dur, tn, dn                             int
		year                                    *int
		expl                                    *bool
		locked, keepForever                     bool
		albumID                                 *uuid.UUID
		libID                                   uuid.UUID
		gain, conf                              *float64
		bitDepth, sampleRate, bitrate, channels *int
		size                                    *int64
		unavailable                             *time.Time
	)
	err = s.Pool.QueryRow(ctx, `
		SELECT t.title, t.duration_ms, t.track_number, t.disc_number, t.year, t.explicit, t.album_id, t.library_id,
		       t.genre_text, t.isrc, t.mbid, t.locked, t.manual_gain_db, t.metadata_source, t.metadata_confidence,
		       coalesce(al.title,''),
		       tf.codec, tf.bit_depth, tf.sample_rate, tf.bitrate, tf.channels, tf.size_bytes, tf.container,
		       t.keep_forever, t.acquisition, t.media_unavailable_at
		FROM tracks t
		LEFT JOIN albums al ON al.id=t.album_id
		LEFT JOIN LATERAL (
			SELECT codec, bit_depth, sample_rate, bitrate, channels, size_bytes, container
			FROM track_files WHERE track_id=t.id AND quality='original' AND deleted_at IS NULL LIMIT 1
		) tf ON TRUE
		WHERE t.id=$1`, id).Scan(
		&title, &dur, &tn, &dn, &year, &expl, &albumID, &libID,
		&genre, &isrc, &mbid, &locked, &gain, &source, &conf,
		&album, &codec, &bitDepth, &sampleRate, &bitrate, &channels, &size, &container,
		&keepForever, &acq, &unavailable)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, 404, "not_found", "track not found")
			return
		}
		writeInternal(w, http.StatusInternalServerError, "db", err.Error())
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
		"keep_forever":         keepForever,
		"acquisition":          acq,
		"media_unavailable":    unavailable != nil,
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
	if !s.requireTrackLibraryWrite(w, r, id) {
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

// autoLockedTrackFields are the track fields a rescan or metadata refresh
// rewrites from file tags or MusicBrainz. A hand edit to one of them records a
// per-field lock so the edit survives; the editor itself still writes them.
var autoLockedTrackFields = map[string]bool{
	"title": true, "artist": true, "album": true, "genre": true,
	"year": true, "disc_number": true, "track_number": true, "mbid": true,
}

// applyTrackMeta writes an editor's changes. Per-field locks guard tracks from
// automated writers, not from editors, so they are not checked here.
func (s *Server) applyTrackMeta(ctx context.Context, id uuid.UUID, body trackMetaBody, userID uuid.UUID) []string {
	var updated, edited []string
	set := func(field, col string, v any) {
		tag, err := s.Pool.Exec(ctx, `UPDATE tracks SET `+col+`=$2, updated_at=now() WHERE id=$1 AND `+col+` IS DISTINCT FROM $2`, id, v)
		if err != nil {
			return
		}
		updated = append(updated, field)
		if tag.RowsAffected() > 0 {
			edited = append(edited, field)
		}
	}
	if body.Title != nil {
		set("title", "title", *body.Title)
	}
	if body.Genre != nil {
		set("genre", "genre_text", *body.Genre)
		s.syncTrackGenres(ctx, id, *body.Genre)
	}
	if body.Year != nil {
		set("year", "year", *body.Year)
	}
	if body.DiscNumber != nil {
		set("disc_number", "disc_number", *body.DiscNumber)
	}
	if body.TrackNumber != nil {
		set("track_number", "track_number", *body.TrackNumber)
	}
	if body.Explicit != nil {
		set("explicit", "explicit", *body.Explicit)
	}
	if body.ISRC != nil {
		set("isrc", "isrc", *body.ISRC)
	}
	if body.MBID != nil {
		set("mbid", "mbid", *body.MBID)
	}
	if body.Locked != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE tracks SET locked=$2, updated_at=now() WHERE id=$1`, id, *body.Locked)
		updated = append(updated, "locked")
	}
	if body.Lyrics != nil {
		_, _ = s.Pool.Exec(ctx, `DELETE FROM lyrics WHERE track_id=$1 AND source='user'`, id)
		if strings.TrimSpace(*body.Lyrics) != "" {
			_, _ = s.Pool.Exec(ctx, `INSERT INTO lyrics (track_id, source, timed, body) VALUES ($1,'user',false,$2)`, id, *body.Lyrics)
		}
		updated = append(updated, "lyrics")
	}
	if body.Artist != nil {
		if s.replaceTrackArtists(ctx, id, *body.Artist) {
			edited = append(edited, "artist")
		}
		updated = append(updated, "artist")
	}
	if body.KeepForever != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE tracks SET keep_forever=$2, updated_at=now() WHERE id=$1`, id, *body.KeepForever)
		updated = append(updated, "keep_forever")
	}
	if body.AlbumID != nil {
		raw := strings.TrimSpace(*body.AlbumID)
		var albumID *uuid.UUID
		ok := raw == ""
		if parsed, err := uuid.Parse(raw); err == nil {
			albumID, ok = &parsed, true
		}
		if ok {
			if tag, err := s.Pool.Exec(ctx, `UPDATE tracks SET album_id=$2, updated_at=now() WHERE id=$1 AND album_id IS DISTINCT FROM $2`, id, albumID); err == nil {
				updated = append(updated, "album")
				if tag.RowsAffected() > 0 {
					edited = append(edited, "album")
				}
			}
		}
	}
	if body.AlbumID == nil && body.Album != nil {
		if s.linkTrackAlbumByTitle(ctx, id, strings.TrimSpace(*body.Album)) {
			updated = append(updated, "album")
			edited = append(edited, "album")
		}
	}
	if body.ManualGainDB != nil {
		if _, err := s.Pool.Exec(ctx, `UPDATE tracks SET manual_gain_db=$2, updated_at=now() WHERE id=$1`, id, *body.ManualGainDB); err == nil {
			updated = append(updated, "manual_gain_db")
		}
	}
	s.lockTrackFields(ctx, []uuid.UUID{id}, userID, edited...)
	return updated
}

// lockTrackFields records per-field locks for hand-edited track fields so a
// rescan or metadata refresh does not undo the edit. Fields no automated
// writer touches are skipped.
func (s *Server) lockTrackFields(ctx context.Context, trackIDs []uuid.UUID, by uuid.UUID, fields ...string) {
	keep := lockableTrackFields(fields)
	if len(keep) == 0 || len(trackIDs) == 0 {
		return
	}
	var lockedBy *uuid.UUID
	if by != uuid.Nil {
		lockedBy = &by
	}
	_, _ = s.Pool.Exec(ctx, lockTrackFieldsSQL, trackIDs, keep, lockedBy)
}

func lockableTrackFields(fields []string) []string {
	var keep []string
	for _, f := range fields {
		if autoLockedTrackFields[f] {
			keep = append(keep, f)
		}
	}
	return keep
}

const lockTrackFieldsSQL = `
	INSERT INTO metadata_locks (entity_type, entity_id, field, locked_by)
	SELECT 'track', t.id, f.field, $3
	FROM tracks t, unnest($2::text[]) AS f(field)
	WHERE t.id = ANY($1)
	ON CONFLICT (entity_type, entity_id, field) DO NOTHING`

// lockAlbumTracks pins every track on albumID to it, so renaming or merging
// the album is not undone by a rescan relinking its tracks to an album named
// after the old file tags.
func (s *Server) lockAlbumTracks(ctx context.Context, albumID, by uuid.UUID) {
	s.lockTrackFields(ctx, s.trackIDsWhere(ctx, `SELECT id FROM tracks WHERE album_id=$1`, albumID), by, "album")
}

// lockArtistTracks pins the credits of every track crediting artistID, so a
// rename or merge is not undone by a rescan re-creating the tagged name.
func (s *Server) lockArtistTracks(ctx context.Context, artistID, by uuid.UUID) {
	s.lockTrackFields(ctx, s.trackIDsWhere(ctx, `SELECT DISTINCT track_id FROM track_artists WHERE artist_id=$1 AND role IN ('primary','featured')`, artistID), by, "artist")
}

func (s *Server) trackIDsWhere(ctx context.Context, sql string, arg any) []uuid.UUID {
	rows, err := s.Pool.Query(ctx, sql, arg)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// linkTrackAlbumByTitle points a track at the album called title in its
// library, preferring one that shares an artist with the track, and creates the
// album when none exists. An empty title detaches the track.
func (s *Server) linkTrackAlbumByTitle(ctx context.Context, trackID uuid.UUID, title string) bool {
	if title == "" {
		_, err := s.Pool.Exec(ctx, `UPDATE tracks SET album_id=NULL, updated_at=now() WHERE id=$1`, trackID)
		return err == nil
	}
	var current string
	_ = s.Pool.QueryRow(ctx, `SELECT coalesce(al.title,'') FROM tracks t LEFT JOIN albums al ON al.id=t.album_id WHERE t.id=$1`, trackID).Scan(&current)
	if current == title {
		return false
	}
	var albumID uuid.UUID
	err := s.Pool.QueryRow(ctx, `
		SELECT al.id FROM albums al, tracks t
		WHERE t.id=$1 AND lower(al.title)=lower($2)
		  AND (al.library_id IS NOT DISTINCT FROM t.library_id)
		ORDER BY EXISTS (
		  SELECT 1 FROM album_artists aa JOIN track_artists ta ON ta.artist_id=aa.artist_id
		  WHERE aa.album_id=al.id AND ta.track_id=t.id
		) DESC, al.created_at
		LIMIT 1`, trackID, title).Scan(&albumID)
	if err != nil {
		if err := s.Pool.QueryRow(ctx, `
			INSERT INTO albums (title, year, library_id)
			SELECT $2, t.year, t.library_id FROM tracks t WHERE t.id=$1
			RETURNING id`, trackID, title).Scan(&albumID); err != nil {
			return false
		}
		_, _ = s.Pool.Exec(ctx, `
			INSERT INTO album_artists (album_id, artist_id, position)
			SELECT $1, ta.artist_id, ta.position FROM track_artists ta WHERE ta.track_id=$2 AND ta.role='primary'
			ON CONFLICT DO NOTHING`, albumID, trackID)
	}
	_, err = s.Pool.Exec(ctx, `UPDATE tracks SET album_id=$2, updated_at=now() WHERE id=$1`, trackID, albumID)
	return err == nil
}

// trackCreditNames is the performer credit the editors show: primary and
// featured artists in credit order.
func (s *Server) trackCreditNames(ctx context.Context, trackID uuid.UUID) []string {
	rows, err := s.Pool.Query(ctx, `
		SELECT ar.name FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
		WHERE ta.track_id=$1 AND ta.role IN ('primary','featured')
		ORDER BY ta.position, ta.role`, trackID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}

// splitArtistNames turns the editor's comma separated artist field into
// distinct names, keeping the first spelling of each.
func splitArtistNames(names string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(names, ",") {
		name := strings.TrimSpace(raw)
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// replaceTrackArtists makes names the track's whole performer credit, all as
// primary artists, which is what Home and the track lists show. Featured rows
// go too because the editors show them in the same field. It reports whether
// the credit changed; an unchanged list is left alone so roles are kept.
func (s *Server) replaceTrackArtists(ctx context.Context, trackID uuid.UUID, names string) bool {
	want := splitArtistNames(names)
	if slices.Equal(want, s.trackCreditNames(ctx, trackID)) {
		return false
	}
	_, _ = s.Pool.Exec(ctx, `DELETE FROM track_artists WHERE track_id=$1 AND role IN ('primary','featured')`, trackID)
	for pos, name := range want {
		var aid uuid.UUID
		err := s.Pool.QueryRow(ctx, `SELECT id FROM artists WHERE lower(name)=lower($1) LIMIT 1`, name).Scan(&aid)
		if err != nil {
			_ = s.Pool.QueryRow(ctx, `INSERT INTO artists (name) VALUES ($1) RETURNING id`, name).Scan(&aid)
		}
		if aid != uuid.Nil {
			_, _ = s.Pool.Exec(ctx, `INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'primary',$3) ON CONFLICT DO NOTHING`, trackID, aid, pos)
		}
	}
	return true
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
		Artist      *string     `json:"artist"`
		Title       *string     `json:"title"`
		AlbumID     *string     `json:"album_id"`
		WriteBack   bool        `json:"write_back"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	for _, id := range body.IDs {
		if !s.requireTrackLibraryWrite(w, r, id) {
			return
		}
	}
	meta := trackMetaBody{
		Genre: body.Genre, Year: body.Year, Explicit: body.Explicit,
		DiscNumber: body.DiscNumber, TrackNumber: body.TrackNumber,
		Artist: body.Artist, Title: body.Title, AlbumID: body.AlbumID, WriteBack: body.WriteBack,
	}
	n, locked := 0, 0
	for _, id := range body.IDs {
		if s.trackGloballyLocked(r.Context(), id) {
			locked++
			continue
		}
		s.applyTrackMeta(r.Context(), id, meta, currentUser(r).ID)
		n++
	}
	out := map[string]any{"updated": n, "locked": locked, "write_back": body.WriteBack}
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
	if !s.requireTrackLibraryWrite(w, r, id) {
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
	if !s.requireAlbumLibrary(w, r, id, "write") {
		return
	}
	var body struct {
		Title       *string `json:"title"`
		Year        *int    `json:"year"`
		Edition     *string `json:"edition_title"`
		Compilation *bool   `json:"is_compilation"`
		Label       *string `json:"label"`
		Artist      *string `json:"artist"`
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
	if body.Artist != nil {
		s.replaceAlbumArtists(ctx, id, *body.Artist)
	}
	if body.Title != nil || body.Artist != nil {
		s.lockAlbumTracks(ctx, id, currentUser(r).ID)
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
	if !s.requireArtistLibrary(w, r, id, "write") {
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
		s.lockArtistTracks(r.Context(), id, currentUser(r).ID)
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
	if !s.requireTrackLibraryWrite(w, r, id) {
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
	if !s.requireAlbumLibrary(w, r, id, "write") {
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
	if !s.requireArtistLibrary(w, r, id, "write") {
		return
	}
	s.saveUploadedArtwork(w, r, "artist", id)
}

// saveUploadedArtwork stores a user-supplied cover. Callers have already checked
// write access to the owning library, which is what lets someone set a cover.
func (s *Server) saveUploadedArtwork(w http.ResponseWriter, r *http.Request, ownerType string, ownerID uuid.UUID) {
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

func (s *Server) deleteTrackArtwork(w http.ResponseWriter, r *http.Request) {
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
	if !s.requireTrackLibraryWrite(w, r, id) {
		return
	}
	if albumID != nil {
		s.removeUploadedArtwork(w, r, "album", *albumID)
		return
	}
	s.removeUploadedArtwork(w, r, "track", id)
}

func (s *Server) deleteAlbumArtwork(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	if !s.requireAlbumLibrary(w, r, id, "write") {
		return
	}
	s.removeUploadedArtwork(w, r, "album", id)
}

func (s *Server) deleteArtistArtwork(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	if !s.requireArtistLibrary(w, r, id, "write") {
		return
	}
	s.removeUploadedArtwork(w, r, "artist", id)
}

// removeUploadedArtwork drops user-supplied covers so the embedded or fetched
// artwork shows again. Files stay on disk; they are keyed by content hash and
// may be shared with other owners.
func (s *Server) removeUploadedArtwork(w http.ResponseWriter, r *http.Request, ownerType string, ownerID uuid.UUID) {
	tag, err := s.Pool.Exec(r.Context(), `DELETE FROM artwork_assets WHERE owner_type=$1 AND owner_id=$2 AND source='user'`, ownerType, ownerID)
	if err != nil {
		writeErr(w, 500, "db", "could not remove artwork")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "removed": tag.RowsAffected(), "owner_type": ownerType, "owner_id": ownerID})
}
