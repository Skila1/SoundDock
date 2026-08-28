package retention

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/mediabusy"
)

func TestCandidatesSkipLiveStreamedTrack(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	liveID, idleID := uuid.New(), uuid.New()
	sid, libID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1, $2, 'managed', $3)`, sid, "live-"+sid.String()[:8], []byte(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1, $2, 'music', $3, '', false)`, libID, "L-"+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms, acquisition, created_at)
		VALUES ($1, $3, 'live', 1000, 'youtube', now() - interval '30 days'),
		       ($2, $3, 'idle', 1000, 'youtube', now() - interval '30 days')`, liveID, idleID, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, quality)
		VALUES ($1, $3, 'live.m4a', 100, 'original'), ($2, $3, 'idle.m4a', 100, 'original')`, liveID, idleID, libID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE track_id IN ($1,$2)`, liveID, idleID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id IN ($1,$2)`, liveID, idleID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
		_, _ = pool.Exec(c, `DELETE FROM server_settings WHERE key=$1`, SettingKey)
	})

	if err := SavePolicy(ctx, pool, Policy{
		Enabled: true, Mode: ModeAge, AgeDays: 1, BatchSize: 50, IntervalMinutes: 1, DryRun: true,
	}); err != nil {
		t.Fatal(err)
	}

	busy := mediabusy.New()
	release := busy.Hold(liveID)
	e := New(pool, nil, nil, t.TempDir(), nil)
	e.SetLive(busy)

	policy := LoadPolicy(ctx, pool)
	cands, err := e.candidates(ctx, policy, 500)
	if err != nil {
		t.Fatal(err)
	}
	foundLive, foundIdle := false, false
	for _, c := range cands {
		if c.ID == liveID {
			foundLive = true
		}
		if c.ID == idleID {
			foundIdle = true
		}
	}
	if foundLive {
		t.Fatal("actively streamed/decoded track must not be a prune candidate")
	}
	if !foundIdle {
		t.Fatal("idle managed youtube track should remain eligible")
	}

	release()
	cands, err = e.candidates(ctx, policy, 500)
	if err != nil {
		t.Fatal(err)
	}
	foundLive = false
	for _, c := range cands {
		if c.ID == liveID {
			foundLive = true
		}
	}
	if !foundLive {
		t.Fatal("after stream/decoder release the track may be eligible again")
	}
}

func TestRetentionInterruptDoesNotTouchHeldMedia(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	trackID, sid, libID := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1, $2, 'managed', $3)`, sid, "hold-"+sid.String()[:8], []byte(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1, $2, 'music', $3, '', false)`, libID, "H-"+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms, acquisition, created_at)
		VALUES ($1, $2, 'busy', 1000, 'youtube', now() - interval '40 days')`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, quality)
		VALUES ($1, $2, 'busy.m4a', 50, 'original')`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM retention_events WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
	})
	if err := SavePolicy(ctx, pool, Policy{
		Enabled: true, Mode: ModeAge, AgeDays: 1, BatchSize: 50, IntervalMinutes: 1, DryRun: false,
	}); err != nil {
		t.Fatal(err)
	}

	purged := false
	e := New(pool, nil, nil, t.TempDir(), func(context.Context, uuid.UUID) (int64, error) {
		purged = true
		return 50, nil
	})
	e.SetLive(mediabusy.New())
	e.Live().Hold(trackID)

	policy := LoadPolicy(ctx, pool)
	cands, err := e.candidates(ctx, policy, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if c.ID == trackID {
			t.Fatal("busy track listed")
		}
	}
	if purged {
		t.Fatal("purge must not run for a held track")
	}
}
