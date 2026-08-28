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

// EnsureStubTrack finds or creates a catalogue row for a YouTube source_ref.
// The stub has no track_files until the fetch job commits.
func EnsureStubTrack(ctx context.Context, pool *pgxpool.Pool, libraryID uuid.UUID, sourceRef string) (uuid.UUID, error) {
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
		return id, nil
	}
	title := "YouTube " + ref
	err = pool.QueryRow(ctx, `
		INSERT INTO tracks (library_id, title, acquisition, acquisition_ref)
		VALUES ($1,$2,'youtube',$3) RETURNING id`, libraryID, title, ref).Scan(&id)
	return id, err
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
