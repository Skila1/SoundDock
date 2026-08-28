package listen

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func listenTrue() *bool {
	v := true
	return &v
}

func seedListenUser(t *testing.T, ctx context.Context, userID uuid.UUID) {
	t.Helper()
	pool := testPool(t)
	username := "w4l-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username); err != nil {
		t.Skip(err)
	}
	if err := EnsureEventsSchema(ctx, pool); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_output_segments WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_instance_state WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_events WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_history WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM play_counts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
}

func fixtureTrack(t *testing.T) uuid.UUID {
	t.Helper()
	pool := testPool(t)
	track := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM tracks WHERE id=$1`, track).Scan(&n); err != nil || n != 1 {
		t.Skip("fixture track missing")
	}
	return track
}

func countKind(t *testing.T, ctx context.Context, userID uuid.UUID, kind string) int {
	t.Helper()
	var n int
	if err := testPool(t).QueryRow(ctx, `SELECT count(*) FROM listen_events WHERE user_id=$1 AND kind=$2`, userID, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestShadowAccumulatedQualifiesWithoutSeek(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := fixtureTrack(t)
	userID := uuid.New()
	seedListenUser(t, ctx, userID)
	instance := uuid.New()
	now := time.Now().UTC()
	for i := 0; i <= 30; i++ {
		cp := Checkpoint{
			TrackID: track, PositionMS: i * 1000, DurationMS: 185000, Source: "web", Kind: "progress",
			PlaybackInstanceID: instance, PlayheadSequence: int64(i + 1),
			RendererKind: "browser", RendererID: "lease", ClientID: "lease",
			Status: "playing", PlaybackRate: 1, At: now.Add(time.Duration(i) * time.Second),
		}
		if err := ApplyShadow(ctx, pool, userID, cp); err != nil {
			t.Fatal(err)
		}
	}
	if got := countKind(t, ctx, userID, "qualify"); got != 1 {
		t.Fatalf("qualify %d", got)
	}
	if got := countKind(t, ctx, userID, "skip"); got != 0 {
		t.Fatalf("skip %d", got)
	}
	var acc int64
	var qp bool
	if err := pool.QueryRow(ctx, `SELECT accumulated_listened_ms, qualified_play FROM listen_events WHERE user_id=$1 AND kind='qualify'`, userID).Scan(&acc, &qp); err != nil {
		t.Fatal(err)
	}
	if acc < 30000 || !qp {
		t.Fatalf("acc=%d qp=%v", acc, qp)
	}
}

func TestShadowSeekJumpDoesNotQualify(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := fixtureTrack(t)
	userID := uuid.New()
	seedListenUser(t, ctx, userID)
	instance := uuid.New()
	now := time.Now().UTC()
	base := Checkpoint{
		TrackID: track, DurationMS: 185000, Source: "web", Kind: "progress",
		PlaybackInstanceID: instance, RendererKind: "browser", RendererID: "lease", ClientID: "lease",
		Status: "playing", PlaybackRate: 1,
	}
	base.PositionMS = 0
	base.PlayheadSequence = 1
	base.At = now
	if err := ApplyShadow(ctx, pool, userID, base); err != nil {
		t.Fatal(err)
	}
	base.PositionMS = 40000
	base.PlayheadSequence = 2
	base.At = now.Add(time.Second)
	if err := ApplyShadow(ctx, pool, userID, base); err != nil {
		t.Fatal(err)
	}
	if got := countKind(t, ctx, userID, "qualify"); got != 0 {
		t.Fatalf("seek must not qualify, got %d", got)
	}
}

func TestShadowSkipBeforeTWritesSkipNotQualify(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := fixtureTrack(t)
	userID := uuid.New()
	seedListenUser(t, ctx, userID)
	instance := uuid.New()
	now := time.Now().UTC()
	cp := Checkpoint{
		TrackID: track, PositionMS: 5000, DurationMS: 185000, Source: "web", Kind: "progress",
		PlaybackInstanceID: instance, PlayheadSequence: 1,
		RendererKind: "browser", RendererID: "lease", ClientID: "lease",
		Status: "playing", At: now,
	}
	if err := ApplyShadow(ctx, pool, userID, cp); err != nil {
		t.Fatal(err)
	}
	cp.Kind = "skip"
	cp.PlayheadSequence = 2
	cp.At = now.Add(time.Second)
	if err := ApplyShadow(ctx, pool, userID, cp); err != nil {
		t.Fatal(err)
	}
	if got := countKind(t, ctx, userID, "qualify"); got != 0 {
		t.Fatalf("qualify %d", got)
	}
	if got := countKind(t, ctx, userID, "skip"); got != 1 {
		t.Fatalf("skip %d", got)
	}
	var qp, skipped bool
	if err := pool.QueryRow(ctx, `SELECT qualified_play, skipped FROM listen_events WHERE user_id=$1 AND kind='skip'`, userID).Scan(&qp, &skipped); err != nil {
		t.Fatal(err)
	}
	if qp || !skipped {
		t.Fatalf("qp=%v skipped=%v", qp, skipped)
	}
}

func TestShadowSkipAfterTSetsBothFlags(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := fixtureTrack(t)
	userID := uuid.New()
	seedListenUser(t, ctx, userID)
	instance := uuid.New()
	now := time.Now().UTC()
	for i := 0; i <= 30; i++ {
		cp := Checkpoint{
			TrackID: track, PositionMS: i * 1000, DurationMS: 185000, Source: "web", Kind: "progress",
			PlaybackInstanceID: instance, PlayheadSequence: int64(i + 1),
			RendererKind: "browser", RendererID: "lease", ClientID: "lease",
			Status: "playing", PlaybackRate: 1, At: now.Add(time.Duration(i) * time.Second),
		}
		if err := ApplyShadow(ctx, pool, userID, cp); err != nil {
			t.Fatal(err)
		}
	}
	skip := Checkpoint{
		TrackID: track, PositionMS: 31000, DurationMS: 185000, Source: "web", Kind: "skip",
		PlaybackInstanceID: instance, PlayheadSequence: 40,
		RendererKind: "browser", RendererID: "lease", ClientID: "lease",
		At: now.Add(31 * time.Second),
	}
	if err := ApplyShadow(ctx, pool, userID, skip); err != nil {
		t.Fatal(err)
	}
	if got := countKind(t, ctx, userID, "qualify"); got != 1 {
		t.Fatalf("qualify %d", got)
	}
	if got := countKind(t, ctx, userID, "skip"); got != 1 {
		t.Fatalf("skip %d", got)
	}
	var qp, skipped bool
	if err := pool.QueryRow(ctx, `SELECT qualified_play, skipped FROM listen_events WHERE user_id=$1 AND kind='skip'`, userID).Scan(&qp, &skipped); err != nil {
		t.Fatal(err)
	}
	if !qp || !skipped {
		t.Fatalf("skip after T qp=%v skipped=%v", qp, skipped)
	}
}

func TestShadowOutputSwitchNoSecondQualify(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := fixtureTrack(t)
	userID := uuid.New()
	seedListenUser(t, ctx, userID)
	instance := uuid.New()
	now := time.Now().UTC()
	for i := 0; i <= 20; i++ {
		cp := Checkpoint{
			TrackID: track, PositionMS: i * 1000, DurationMS: 185000, Source: "web", Kind: "progress",
			PlaybackInstanceID: instance, PlayheadSequence: int64(i + 1),
			RendererKind: "browser", RendererID: "lease", ClientID: "lease",
			Status: "playing", PlaybackRate: 1, At: now.Add(time.Duration(i) * time.Second),
		}
		if err := ApplyShadow(ctx, pool, userID, cp); err != nil {
			t.Fatal(err)
		}
	}
	for i := 21; i <= 30; i++ {
		cp := Checkpoint{
			TrackID: track, PositionMS: i * 1000, DurationMS: 185000, Source: "discord", Kind: "progress",
			PlaybackInstanceID: instance, PlayheadSequence: int64(i + 1),
			RendererKind: "discord", AudioListener: listenTrue(),
			Status: "playing", PlaybackRate: 1, At: now.Add(time.Duration(i) * time.Second),
		}
		if err := ApplyShadow(ctx, pool, userID, cp); err != nil {
			t.Fatal(err)
		}
	}
	if got := countKind(t, ctx, userID, "qualify"); got != 1 {
		t.Fatalf("qualify after output switch %d", got)
	}
	var segs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM listen_output_segments WHERE user_id=$1 AND playback_instance_id=$2`, userID, instance).Scan(&segs); err != nil {
		t.Fatal(err)
	}
	if segs != 2 {
		t.Fatalf("segments %d want 2", segs)
	}
	var openOut string
	var ended int
	if err := pool.QueryRow(ctx, `
		SELECT output, (SELECT count(*) FROM listen_output_segments WHERE user_id=$1 AND ended_at IS NOT NULL)
		FROM listen_output_segments WHERE user_id=$1 AND ended_at IS NULL`, userID).Scan(&openOut, &ended); err != nil {
		t.Fatal(err)
	}
	if openOut != "discord" || ended != 1 {
		t.Fatalf("open=%s ended=%d", openOut, ended)
	}
}

func TestShadowUniqueConflictIdempotent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := fixtureTrack(t)
	userID := uuid.New()
	seedListenUser(t, ctx, userID)
	instance := uuid.New()
	now := time.Now().UTC()
	for i := 0; i <= 30; i++ {
		cp := Checkpoint{
			TrackID: track, PositionMS: i * 1000, DurationMS: 185000, Source: "web", Kind: "progress",
			PlaybackInstanceID: instance, PlayheadSequence: int64(i + 1),
			RendererKind: "browser", RendererID: "lease", ClientID: "lease",
			Status: "playing", PlaybackRate: 1, At: now.Add(time.Duration(i) * time.Second),
		}
		if err := ApplyShadow(ctx, pool, userID, cp); err != nil {
			t.Fatal(err)
		}
	}
	dup := Checkpoint{
		TrackID: track, PositionMS: 31000, DurationMS: 185000, Source: "web", Kind: "progress",
		PlaybackInstanceID: instance, PlayheadSequence: 99,
		RendererKind: "browser", RendererID: "lease", ClientID: "lease",
		Status: "playing", PlaybackRate: 1, At: now.Add(31 * time.Second),
	}
	if err := ApplyShadow(ctx, pool, userID, dup); err != nil {
		t.Fatal(err)
	}
	st := FSM{TrackID: track, AccumulatedMS: 30000, Qualified: true, StartedAt: now}
	if err := insertEvent(ctx, pool, userID, dup, st, "qualify"); err != nil {
		t.Fatal(err)
	}
	if got := countKind(t, ctx, userID, "qualify"); got != 1 {
		t.Fatalf("idempotent qualify %d", got)
	}
}

func TestBackfillListenedMsNull(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := fixtureTrack(t)
	userID := uuid.New()
	seedListenUser(t, ctx, userID)
	if _, err := pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, played_at, duration_ms, source)
		VALUES ($1,$2,now(),185000,'web')`, userID, track); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO listen_events (
			user_id, track_id, kind, accumulated_listened_ms, listened_ms, track_duration_ms,
			qualified_play, skipped, legacy_backfill, source, started_at
		)
		SELECT h.user_id, h.track_id, 'qualify', 0, NULL, h.duration_ms, TRUE, FALSE, TRUE,
			CASE WHEN h.source IN ('web', 'discord', 'import') THEN h.source ELSE 'web' END,
			h.played_at
		FROM listen_history h WHERE h.user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	var listened *int
	var acc int64
	var backfill, qp bool
	if err := pool.QueryRow(ctx, `
		SELECT listened_ms, accumulated_listened_ms, legacy_backfill, qualified_play
		FROM listen_events WHERE user_id=$1 AND legacy_backfill`, userID).Scan(&listened, &acc, &backfill, &qp); err != nil {
		t.Fatal(err)
	}
	if listened != nil {
		t.Fatalf("backfill listened_ms=%v want NULL", *listened)
	}
	if acc != 0 || !backfill || !qp {
		t.Fatalf("acc=%d backfill=%v qp=%v", acc, backfill, qp)
	}
}

func TestShadowSpectatorTabDoesNotCredit(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := fixtureTrack(t)
	userID := uuid.New()
	seedListenUser(t, ctx, userID)
	instance := uuid.New()
	now := time.Now().UTC()
	for i := 0; i <= 30; i++ {
		cp := Checkpoint{
			TrackID: track, PositionMS: i * 1000, DurationMS: 185000, Source: "web", Kind: "progress",
			PlaybackInstanceID: instance, PlayheadSequence: int64(i + 1),
			RendererKind: "browser", RendererID: "lease", ClientID: "other-tab",
			Status: "playing", PlaybackRate: 1, At: now.Add(time.Duration(i) * time.Second),
		}
		if err := ApplyShadow(ctx, pool, userID, cp); err != nil {
			t.Fatal(err)
		}
	}
	if got := countKind(t, ctx, userID, "qualify"); got != 0 {
		t.Fatalf("spectator qualified %d", got)
	}
}
