package external

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/scapex"
)

// FetchJobType is the job that downloads a YouTube pick onto its stub.
const FetchJobType = "scapex.fetch"

const maxItemErrorRunes = 200

type fetchJobPayload struct {
	URLs          []string `json:"urls"`
	SourceRefs    []string `json:"source_refs"`
	CoalesceKey   string   `json:"coalesce_key"`
	DestLibraryID string   `json:"dest_library_id"`
}

func (p fetchJobPayload) refs() []string {
	raw := p.SourceRefs
	if len(raw) == 0 {
		raw = p.URLs
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range raw {
		ref := scapex.CanonicalSourceRef(r)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

func (p fetchJobPayload) library() uuid.UUID {
	id, _ := uuid.Parse(strings.TrimSpace(p.DestLibraryID))
	return id
}

// DownloadFailureReason turns a stored job error into the short text shown on
// a Needs review item, e.g. "Download failed: Video unavailable".
func DownloadFailureReason(msg string) string {
	line := ""
	for _, l := range strings.Split(msg, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || l == jobs.ErrTerminal.Error() {
			continue
		}
		if strings.Contains(l, "ERROR:") {
			line = l
			break
		}
		if line == "" {
			line = l
		}
	}
	if i := strings.LastIndex(line, "ERROR:"); i >= 0 {
		line = strings.TrimSpace(line[i+len("ERROR:"):])
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "yt-dlp:"))
	// "[youtube] abc123: Sign in to confirm your age" -> "Sign in to confirm your age"
	if strings.HasPrefix(line, "[") {
		if i := strings.Index(line, ": "); i > 0 {
			line = strings.TrimSpace(line[i+2:])
		}
	}
	if line == "" {
		line = "unknown error"
	}
	if utf8.RuneCountInString(line) > maxItemErrorRunes {
		line = string([]rune(line)[:maxItemErrorRunes]) + "..."
	}
	return "Download failed: " + line
}

// ConfirmFetchJob runs when a scapex.fetch job completes. Import items still
// pointing at a file-less stub for the video move to the track that holds the
// download, and stale item errors clear.
func ConfirmFetchJob(ctx context.Context, pool *pgxpool.Pool, job jobs.Job) {
	p, ok := parseFetchJob(pool, job)
	if !ok {
		return
	}
	touched := map[uuid.UUID]uuid.UUID{}
	for _, ref := range p.refs() {
		confirmFetchedRef(ctx, pool, p.library(), ref, touched)
	}
	notifyTouched(ctx, pool, touched)
}

// FailFetchJob runs when a scapex.fetch job fails for good. Import items
// mapped to the dead stub go back to unmatched with a short reason and the
// stub leaves the imported SoundDock playlist, so the song shows up in Needs
// review instead of as a silent unplayable entry.
func FailFetchJob(ctx context.Context, pool *pgxpool.Pool, job jobs.Job, msg string) {
	p, ok := parseFetchJob(pool, job)
	if !ok || fetchStillActive(ctx, pool, job.ID, p.CoalesceKey) {
		return
	}
	reason := DownloadFailureReason(msg)
	touched := map[uuid.UUID]uuid.UUID{}
	for _, ref := range p.refs() {
		// The video may still be on disk from another fetch; then the stubs
		// just need pointing at it.
		if confirmFetchedRef(ctx, pool, p.library(), ref, touched) {
			continue
		}
		for _, stub := range deadStubs(ctx, pool, p.library(), ref, job.ID) {
			unmapDeadStub(ctx, pool, stub, reason, touched)
		}
	}
	notifyTouched(ctx, pool, touched)
}

func parseFetchJob(pool *pgxpool.Pool, job jobs.Job) (fetchJobPayload, bool) {
	var p fetchJobPayload
	if pool == nil || job.Type != FetchJobType {
		return p, false
	}
	if json.Unmarshal(job.Payload, &p) != nil {
		return p, false
	}
	return p, len(p.refs()) > 0
}

func notifyTouched(ctx context.Context, pool *pgxpool.Pool, touched map[uuid.UUID]uuid.UUID) {
	for pl, user := range touched {
		notifyPlaylistInvalidate(ctx, pool, user, pl)
	}
}

// fetchStillActive reports a newer fetch of the same video that may still fill the stub.
func fetchStillActive(ctx context.Context, pool *pgxpool.Pool, jobID uuid.UUID, key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	var n int
	_ = pool.QueryRow(ctx, `
		SELECT count(*) FROM jobs
		WHERE type=$1 AND id<>$2 AND status IN ('queued','running','retry')
		  AND payload->>'coalesce_key'=$3`, FetchJobType, jobID, key).Scan(&n)
	return n > 0
}

// deadStubs are the file-less catalogue rows for ref: stubs by acquisition_ref
// plus any intent tracks of the job.
func deadStubs(ctx context.Context, pool *pgxpool.Pool, lib uuid.UUID, ref string, jobID uuid.UUID) []uuid.UUID {
	var libArg any
	if lib != uuid.Nil {
		libArg = lib
	}
	rows, err := pool.Query(ctx, `
		SELECT t.id FROM tracks t
		WHERE ((t.acquisition_ref=$1 AND ($2::uuid IS NULL OR t.library_id=$2))
		    OR t.id IN (SELECT i.track_id FROM acquisition_intents i WHERE i.job_id=$3 AND i.source_ref=$1))
		  AND NOT EXISTS (SELECT 1 FROM track_files tf WHERE tf.track_id=t.id AND tf.deleted_at IS NULL)`,
		ref, libArg, jobID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// importedPlaylists lists SoundDock playlists whose import maps an item to trackID.
func importedPlaylists(ctx context.Context, pool *pgxpool.Pool, trackID uuid.UUID) map[uuid.UUID]uuid.UUID {
	out := map[uuid.UUID]uuid.UUID{}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT p.sounddock_playlist_id, p.user_id
		FROM external_playlist_items i
		JOIN external_playlists p ON p.id=i.external_playlist_id
		WHERE i.mapped_track_id=$1 AND p.sounddock_playlist_id IS NOT NULL`, trackID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var pl, user uuid.UUID
		if rows.Scan(&pl, &user) == nil {
			out[pl] = user
		}
	}
	return out
}

func unmapDeadStub(ctx context.Context, pool *pgxpool.Pool, stub uuid.UUID, reason string, touched map[uuid.UUID]uuid.UUID) {
	playlists := importedPlaylists(ctx, pool, stub)
	tag, err := pool.Exec(ctx, `
		UPDATE external_playlist_items
		SET mapped_track_id=NULL, match_status='unmatched', match_confidence=0, item_error=$2
		WHERE mapped_track_id=$1`, stub, reason)
	if err != nil || tag.RowsAffected() == 0 {
		return
	}
	_, _ = pool.Exec(ctx, `DELETE FROM external_track_mappings WHERE sounddock_track_id=$1 AND mapping_source='youtube'`, stub)
	for pl, user := range playlists {
		del, err := pool.Exec(ctx, `DELETE FROM playlist_entries WHERE playlist_id=$1 AND track_id=$2`, pl, stub)
		if err == nil && del.RowsAffected() > 0 {
			renumberPlaylist(ctx, pool, pl)
		}
		touched[pl] = user
	}
}

// renumberPlaylist closes position gaps; reconcilePlaylistEntries relies on
// positions matching list order.
func renumberPlaylist(ctx context.Context, pool *pgxpool.Pool, playlistID uuid.UUID) {
	_, _ = pool.Exec(ctx, `
		UPDATE playlist_entries pe SET position=r.pos
		FROM (
		  SELECT id, row_number() OVER (ORDER BY position, added_at) - 1 AS pos
		  FROM playlist_entries WHERE playlist_id=$1
		) r
		WHERE pe.id=r.id AND pe.playlist_id=$1 AND pe.position<>r.pos`, playlistID)
}

// confirmFetchedRef points items and imported playlist rows at the track that
// now holds the download for ref and clears any stale item error.
func confirmFetchedRef(ctx context.Context, pool *pgxpool.Pool, lib uuid.UUID, ref string, touched map[uuid.UUID]uuid.UUID) bool {
	var libArg any
	if lib != uuid.Nil {
		libArg = lib
	}
	var real uuid.UUID
	err := pool.QueryRow(ctx, `
		SELECT t.id FROM tracks t
		JOIN track_files tf ON tf.track_id=t.id AND tf.quality='original' AND tf.deleted_at IS NULL
		WHERE t.acquisition_ref=$1 AND ($2::uuid IS NULL OR t.library_id=$2)
		ORDER BY t.created_at DESC LIMIT 1`, ref, libArg).Scan(&real)
	if err != nil || real == uuid.Nil {
		return false
	}
	for _, stub := range deadStubs(ctx, pool, lib, ref, uuid.Nil) {
		if stub == real {
			continue
		}
		for pl, user := range importedPlaylists(ctx, pool, stub) {
			_, _ = pool.Exec(ctx, `UPDATE playlist_entries SET track_id=$3 WHERE playlist_id=$1 AND track_id=$2`, pl, stub, real)
			touched[pl] = user
		}
		_, _ = pool.Exec(ctx, `UPDATE external_playlist_items SET mapped_track_id=$2 WHERE mapped_track_id=$1`, stub, real)
		_, _ = pool.Exec(ctx, `UPDATE external_track_mappings SET sounddock_track_id=$2 WHERE sounddock_track_id=$1 AND mapping_source='youtube'`, stub, real)
	}
	tag, _ := pool.Exec(ctx, `
		UPDATE external_playlist_items SET item_error=''
		WHERE mapped_track_id=$1 AND item_error<>''`, real)
	if tag.RowsAffected() > 0 {
		for pl, user := range importedPlaylists(ctx, pool, real) {
			touched[pl] = user
		}
	}
	return true
}

func withoutID(ids []uuid.UUID, drop uuid.UUID) []uuid.UUID {
	out := ids[:0]
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

// fetchFailure reports whether the download behind a YouTube stub already
// failed for good (or the stub is gone), with the Needs review reason. The
// import uses it for picks whose fetch finished before the import committed.
func fetchFailure(ctx context.Context, q execer, stub uuid.UUID) (string, bool) {
	if stub == uuid.Nil {
		return "", false
	}
	var ref string
	var files int
	err := q.QueryRow(ctx, `
		SELECT t.acquisition_ref,
		  (SELECT count(*) FROM track_files tf WHERE tf.track_id=t.id AND tf.deleted_at IS NULL)
		FROM tracks t WHERE t.id=$1`, stub).Scan(&ref, &files)
	if err != nil {
		return DownloadFailureReason("track was removed"), true
	}
	if files > 0 || ref == "" {
		return "", false
	}
	var status, msg string
	err = q.QueryRow(ctx, `
		SELECT status, coalesce(last_error,'') FROM jobs
		WHERE type=$1 AND payload->'source_refs' ? $2
		ORDER BY created_at DESC LIMIT 1`, FetchJobType, ref).Scan(&status, &msg)
	if err != nil || status != "failed" {
		return "", false
	}
	return DownloadFailureReason(msg), true
}
