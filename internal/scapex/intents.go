package scapex

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	IntentPlay  = "play"
	IntentQueue = "queue"
	IntentNext  = "next"

	StatusQueued      = "queued"
	StatusDownloading = "downloading"
	StatusProcessing  = "processing"
	StatusScanning    = "scanning"
	StatusReady       = "ready"
	StatusApplied     = "applied"
	StatusFailed      = "failed"
	StatusCancelled   = "cancelled"
	StatusStale       = "stale"

	ProviderYouTube = "youtube"
	ProviderScapeX  = "scapex"
)

// IntentInput is the snapshot taken when a play/queue request arrives.
// W6-http can stash this on the request context via WithIntentInput.
type IntentInput struct {
	UserID                uuid.UUID
	SessionID             uuid.UUID
	TrackID               uuid.UUID
	Intent                string
	SourceRef             string
	Provider              string
	DestLibraryID         uuid.UUID
	MediaPolicyID         string
	ExpectedStateRevision int64
	ExpectedInstanceID    uuid.UUID
	QueueAfterItemID      uuid.UUID
	CorrelationID         string
}

type Intent struct {
	ID                    uuid.UUID
	JobID                 uuid.UUID
	UserID                uuid.UUID
	SessionID             uuid.UUID
	TrackID               uuid.UUID
	Intent                string
	SourceRef             string
	Provider              string
	DestLibraryID         uuid.UUID
	MediaPolicyID         string
	ExpectedStateRevision int64
	ExpectedInstanceID    uuid.UUID
	QueueAfterItemID      uuid.UUID
	Status                string
	CorrelationID         string
}

func NormalizeIntent(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case IntentPlay:
		return IntentPlay
	case IntentNext:
		return IntentNext
	default:
		return IntentQueue
	}
}

func NormalizeProvider(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ProviderScapeX:
		return ProviderScapeX
	default:
		return ProviderYouTube
	}
}

func CanonicalSourceRef(raw string) string {
	raw = strings.TrimSpace(raw)
	if id := VideoID(raw); id != "" {
		return id
	}
	return raw
}

// TrackHint is display metadata already known from search (or a pasted hit).
// Stubs use this so the queue never shows a YouTube id as the title.
type TrackHint struct {
	Title      string
	Artist     string
	DurationMS int
}

func IsPlaceholderTitle(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || strings.EqualFold(s, "Restoring") || strings.HasPrefix(s, "YouTube ")
}

// IsWeakTaglessTitle is a yt-dlp filename used as a title (videoId or videoId.m4a).
func IsWeakTaglessTitle(title, videoID string) bool {
	title = strings.TrimSpace(title)
	videoID = strings.TrimSpace(videoID)
	if title == "" || videoID == "" {
		return title == ""
	}
	base := title
	if i := strings.LastIndex(base, "."); i > 0 {
		ext := strings.ToLower(base[i:])
		if ext == ".m4a" || ext == ".opus" || ext == ".webm" || ext == ".mp3" || ext == ".ogg" {
			base = base[:i]
		}
	}
	return strings.EqualFold(base, videoID)
}

func stubTitle(ref string, hint TrackHint) string {
	if t := strings.TrimSpace(hint.Title); t != "" && !IsPlaceholderTitle(t) {
		return t
	}
	if ref != "" {
		return "YouTube " + ref
	}
	return "Restoring"
}

// EnsureStubTrack finds or creates a catalogue row for a YouTube source_ref.
// The stub has no track_files until the fetch job commits. Known search
// metadata is written immediately so the queue can show the real name.
func EnsureStubTrack(ctx context.Context, pool *pgxpool.Pool, libraryID uuid.UUID, sourceRef string, hint TrackHint) (uuid.UUID, error) {
	if pool == nil {
		return uuid.Nil, fmt.Errorf("no database")
	}
	ref := CanonicalSourceRef(sourceRef)
	if ref == "" {
		return uuid.Nil, fmt.Errorf("source_ref required")
	}
	if libraryID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("dest library required")
	}
	var id uuid.UUID
	err := pool.QueryRow(ctx, `
		SELECT id FROM tracks
		WHERE library_id=$1 AND acquisition_ref=$2
		ORDER BY created_at DESC LIMIT 1`, libraryID, ref).Scan(&id)
	if err == nil {
		ApplyStubHint(ctx, pool, id, hint)
		return id, nil
	}
	title := stubTitle(ref, hint)
	dur := hint.DurationMS
	if dur < 0 {
		dur = 0
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO tracks (library_id, title, duration_ms, acquisition, acquisition_ref)
		VALUES ($1,$2,$3,'youtube',$4) RETURNING id`, libraryID, title, dur, ref).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	setStubArtists(ctx, pool, id, hint.Artist)
	return id, nil
}

func ApplyStubHint(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, hint TrackHint) {
	if pool == nil || id == uuid.Nil {
		return
	}
	title := strings.TrimSpace(hint.Title)
	if title != "" && !IsPlaceholderTitle(title) {
		if hint.DurationMS > 0 {
			_, _ = pool.Exec(ctx, `
				UPDATE tracks SET title=$2, duration_ms=CASE WHEN duration_ms=0 THEN $3 ELSE duration_ms END, updated_at=now()
				WHERE id=$1`, id, title, hint.DurationMS)
		} else {
			_, _ = pool.Exec(ctx, `UPDATE tracks SET title=$2, updated_at=now() WHERE id=$1`, id, title)
		}
	} else if hint.DurationMS > 0 {
		_, _ = pool.Exec(ctx, `UPDATE tracks SET duration_ms=CASE WHEN duration_ms=0 THEN $2 ELSE duration_ms END, updated_at=now() WHERE id=$1`, id, hint.DurationMS)
	}
	if strings.TrimSpace(hint.Artist) != "" {
		setStubArtists(ctx, pool, id, hint.Artist)
	}
}

func setStubArtists(ctx context.Context, pool *pgxpool.Pool, trackID uuid.UUID, names string) {
	if pool == nil || trackID == uuid.Nil {
		return
	}
	_, _ = pool.Exec(ctx, `DELETE FROM track_artists WHERE track_id=$1 AND role='primary'`, trackID)
	pos := 0
	for _, raw := range strings.Split(names, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		var aid uuid.UUID
		err := pool.QueryRow(ctx, `SELECT id FROM artists WHERE lower(name)=lower($1) LIMIT 1`, name).Scan(&aid)
		if err != nil {
			_ = pool.QueryRow(ctx, `INSERT INTO artists (name) VALUES ($1) RETURNING id`, name).Scan(&aid)
		}
		if aid != uuid.Nil {
			_, _ = pool.Exec(ctx, `INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'primary',$3) ON CONFLICT DO NOTHING`, trackID, aid, pos)
			pos++
		}
	}
}

func InsertIntent(ctx context.Context, pool *pgxpool.Pool, in IntentInput, jobID uuid.UUID) (uuid.UUID, error) {
	if pool == nil {
		return uuid.Nil, fmt.Errorf("no database")
	}
	if in.UserID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("user_id required")
	}
	intent := NormalizeIntent(in.Intent)
	provider := NormalizeProvider(in.Provider)
	ref := CanonicalSourceRef(in.SourceRef)
	policy := NormalizePolicy(in.MediaPolicyID)
	corr := strings.TrimSpace(in.CorrelationID)
	if corr == "" {
		corr = uuid.NewString()
	}
	var job any
	if jobID != uuid.Nil {
		job = jobID
	}
	var session, instance, after, track any
	if in.SessionID != uuid.Nil {
		session = in.SessionID
	}
	if in.ExpectedInstanceID != uuid.Nil {
		instance = in.ExpectedInstanceID
	}
	if in.QueueAfterItemID != uuid.Nil {
		after = in.QueueAfterItemID
	}
	if in.TrackID != uuid.Nil {
		track = in.TrackID
	}
	var id uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO acquisition_intents (
			job_id, user_id, session_id, track_id, intent, source_ref, provider,
			dest_library_id, media_policy_id, expected_state_revision, expected_instance_id,
			queue_after_item_id, status, correlation_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id`,
		job, in.UserID, session, track, intent, ref, provider,
		in.DestLibraryID, policy, in.ExpectedStateRevision, instance,
		after, StatusQueued, corr,
	).Scan(&id)
	return id, err
}

func SetIntentsStatus(ctx context.Context, pool *pgxpool.Pool, jobID uuid.UUID, status string, from []string) error {
	if pool == nil || jobID == uuid.Nil {
		return nil
	}
	if len(from) == 0 {
		_, err := pool.Exec(ctx, `
			UPDATE acquisition_intents SET status=$2, updated_at=now()
			WHERE job_id=$1 AND status NOT IN ('applied','failed','cancelled','stale')`, jobID, status)
		return err
	}
	_, err := pool.Exec(ctx, `
		UPDATE acquisition_intents SET status=$2, updated_at=now()
		WHERE job_id=$1 AND status = ANY($3)`, jobID, status, from)
	return err
}

func FailJobIntents(ctx context.Context, pool *pgxpool.Pool, jobID uuid.UUID, msg string) error {
	if pool == nil || jobID == uuid.Nil {
		return nil
	}
	_, err := pool.Exec(ctx, `
		UPDATE acquisition_intents SET status=$2, error=$3, updated_at=now()
		WHERE job_id=$1 AND status NOT IN ('applied','cancelled','stale')`,
		jobID, StatusFailed, strings.TrimSpace(msg))
	return err
}

// DropFailedAcquire is the historical name for DropUncommitted.
func DropFailedAcquire(ctx context.Context, pool *pgxpool.Pool, play Player, jobID uuid.UUID) {
	DropUncommitted(ctx, pool, play, jobID)
}

// DropUncommitted removes failed/cancelled stubs that never received a file,
// except playlist-held rows which stay as failed catalogue entries.
func DropUncommitted(ctx context.Context, pool *pgxpool.Pool, play Player, jobID uuid.UUID) {
	if pool == nil || jobID == uuid.Nil {
		return
	}
	intents, err := ListJobIntents(ctx, pool, jobID)
	if err != nil {
		return
	}
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, in := range intents {
		if in.TrackID == uuid.Nil || seen[in.TrackID] {
			continue
		}
		seen[in.TrackID] = true
		ids = append(ids, in.TrackID)
	}
	GCUnreferencedStubs(ctx, pool, play, ids)
}

func playlistHeld(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) bool {
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM playlist_entries WHERE track_id=$1`, id).Scan(&n)
	return n > 0
}

func queueHeld(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) bool {
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM playback_queue_items WHERE track_id=$1`, id).Scan(&n)
	return n > 0
}

// GCUnreferencedStubs deletes failed/cancelled stubs with no original, no
// playlist row, and no queue occurrence. Playlist-held stubs stay.
func GCUnreferencedStubs(ctx context.Context, pool *pgxpool.Pool, play Player, ids []uuid.UUID) {
	if pool == nil || len(ids) == 0 {
		return
	}
	var drop []uuid.UUID
	for _, id := range ids {
		var files int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM track_files WHERE track_id=$1 AND deleted_at IS NULL`, id).Scan(&files)
		if files > 0 {
			continue
		}
		if playlistHeld(ctx, pool, id) {
			continue
		}
		drop = append(drop, id)
	}
	if len(drop) == 0 {
		return
	}
	if play != nil {
		_ = play.DropTracks(ctx, drop)
	} else {
		_, _ = pool.Exec(ctx, `DELETE FROM playback_queue_items WHERE track_id = ANY($1)`, drop)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM tracks WHERE id = ANY($1) AND acquisition IN ('youtube','scapex')`, drop)
}

// SweepUnreferencedFailedStubs GCs failed stubs older than 30 days that nothing references.
func SweepUnreferencedFailedStubs(ctx context.Context, pool *pgxpool.Pool, play Player) {
	if pool == nil {
		return
	}
	rows, err := pool.Query(ctx, `
		SELECT t.id FROM tracks t
		WHERE t.acquisition IN ('youtube','scapex')
		  AND t.created_at < now() - interval '30 days'
		  AND NOT EXISTS (
		    SELECT 1 FROM track_files tf WHERE tf.track_id=t.id AND tf.deleted_at IS NULL
		  )
		  AND NOT EXISTS (SELECT 1 FROM playlist_entries pe WHERE pe.track_id=t.id)
		  AND NOT EXISTS (SELECT 1 FROM playback_queue_items q WHERE q.track_id=t.id)
		  AND EXISTS (
		    SELECT 1 FROM acquisition_intents i
		    WHERE i.track_id=t.id AND i.status IN ('failed','cancelled')
		  )`)
	if err != nil {
		return
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	GCUnreferencedStubs(ctx, pool, play, ids)
}

func RecordTrackSource(ctx context.Context, pool *pgxpool.Pool, trackID uuid.UUID, provider, sourceRef string, jobID uuid.UUID) error {
	if pool == nil || trackID == uuid.Nil || sourceRef == "" {
		return nil
	}
	var job any
	if jobID != uuid.Nil {
		job = jobID
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO track_sources (track_id, provider, source_ref, job_id)
		VALUES ($1,$2,$3,$4)`, trackID, NormalizeProvider(provider), CanonicalSourceRef(sourceRef), job)
	return err
}

func ListJobIntents(ctx context.Context, pool *pgxpool.Pool, jobID uuid.UUID) ([]Intent, error) {
	if pool == nil || jobID == uuid.Nil {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT id, coalesce(job_id, '00000000-0000-0000-0000-000000000000'::uuid), user_id,
			coalesce(session_id, '00000000-0000-0000-0000-000000000000'::uuid),
			coalesce(track_id, '00000000-0000-0000-0000-000000000000'::uuid),
			intent, source_ref, provider, dest_library_id, media_policy_id,
			expected_state_revision,
			coalesce(expected_instance_id, '00000000-0000-0000-0000-000000000000'::uuid),
			coalesce(queue_after_item_id, '00000000-0000-0000-0000-000000000000'::uuid),
			status, coalesce(correlation_id,'')
		FROM acquisition_intents
		WHERE job_id=$1
		ORDER BY created_at`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Intent
	for rows.Next() {
		var in Intent
		if err := rows.Scan(&in.ID, &in.JobID, &in.UserID, &in.SessionID, &in.TrackID,
			&in.Intent, &in.SourceRef, &in.Provider, &in.DestLibraryID, &in.MediaPolicyID,
			&in.ExpectedStateRevision, &in.ExpectedInstanceID, &in.QueueAfterItemID,
			&in.Status, &in.CorrelationID); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func UserMayAccessLibrary(ctx context.Context, pool *pgxpool.Pool, userID, libraryID uuid.UUID) (bool, error) {
	if pool == nil || userID == uuid.Nil {
		return false, nil
	}
	var disabled bool
	err := pool.QueryRow(ctx, `SELECT disabled FROM users WHERE id=$1`, userID).Scan(&disabled)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	if disabled {
		return false, nil
	}
	if libraryID == uuid.Nil {
		return true, nil
	}
	var grants int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM library_grants WHERE library_id=$1`, libraryID).Scan(&grants); err != nil {
		return false, err
	}
	if grants == 0 {
		return true, nil
	}
	var ok bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM library_grants g
			WHERE g.library_id=$1 AND (
				g.user_id=$2
				OR g.role_id IN (SELECT role_id FROM user_roles WHERE user_id=$2)
			)
			AND (g.actions IS NULL OR cardinality(g.actions)=0
				OR g.actions && ARRAY['read','stream','write','admin'])
		)`, libraryID, userID).Scan(&ok)
	return ok, err
}

func sessionExists(ctx context.Context, pool *pgxpool.Pool, sessionID uuid.UUID) bool {
	if pool == nil || sessionID == uuid.Nil {
		return false
	}
	var ok bool
	_ = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM playback_sessions WHERE id=$1)`, sessionID).Scan(&ok)
	return ok
}

func setIntent(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, status, errMsg string) {
	if pool == nil || id == uuid.Nil {
		return
	}
	_, _ = pool.Exec(ctx, `
		UPDATE acquisition_intents SET status=$2, error=$3, updated_at=now() WHERE id=$1`,
		id, status, errMsg)
}
