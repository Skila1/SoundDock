package playback

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	username := "w1a-" + userID.String()[:8]
	if _, err := pool.Exec(context.Background(), `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM playback_command_receipts WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_voice_runtime WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM playback_sessions WHERE user_id=$1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID)
	})
	return userID
}

func revOf(q map[string]any) int64 {
	switch v := q["state_revision"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func seqOf(q map[string]any) int64 {
	switch v := q["playhead_sequence"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func instanceOf(q map[string]any) uuid.UUID {
	switch v := q["playback_instance_id"].(type) {
	case uuid.UUID:
		return v
	case *uuid.UUID:
		if v != nil {
			return *v
		}
	}
	return uuid.Nil
}

func genOf(q map[string]any) int64 {
	switch v := q["renderer_generation"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func fixtureTracks(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	a := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	b := uuid.MustParse("00000000-0000-4000-8000-000000000051")
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM tracks WHERE id IN ($1,$2)`, a, b).Scan(&n); err != nil || n != 2 {
		t.Skip("fixture tracks missing")
	}
	return a, b
}

func TestRevisionNotBumpedByPlayheadOrHeartbeat(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a, b}, 0); err != nil {
		t.Fatal(err)
	}
	gen, err := e.AcquireBrowserRenderer(ctx, sid, "client-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	rev := revOf(q)
	seq := seqOf(q)
	inst := instanceOf(q)
	if inst == uuid.Nil {
		t.Fatal("expected instance")
	}
	if err := e.CheckpointPlayhead(ctx, sid, inst, 1200); err != nil {
		t.Fatal(err)
	}
	if err := e.HeartbeatRenderer(ctx, sid, RendererBrowser, "client-1", gen); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if revOf(q) != rev {
		t.Fatalf("state_revision bumped by checkpoint/heartbeat: %d -> %d", rev, revOf(q))
	}
	if seqOf(q) != seq+1 {
		t.Fatalf("playhead_sequence %d want %d", seqOf(q), seq+1)
	}
}

func TestBindDiscordRendererAtomicOneWinner(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userA := seedUser(t, pool)
	userB := seedUser(t, pool)
	sidA, err := e.WebSession(ctx, userA, "a")
	if err != nil {
		t.Fatal(err)
	}
	sidB, err := e.WebSession(ctx, userB, "b")
	if err != nil {
		t.Fatal(err)
	}
	guild := "w1a-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	sids := []uuid.UUID{sidA, sidB}
	wg.Add(2)
	for i, sid := range sids {
		go func(i int, sid uuid.UUID) {
			defer wg.Done()
			_, errs[i] = e.BindDiscordRenderer(ctx, guild, sid, "channel-1", 0, "bot", 1)
		}(i, sid)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("bind %d: %v", i, err)
		}
	}

	var bound uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT session_id FROM discord_voice_runtime WHERE guild_id=$1`, guild).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if bound != sidA && bound != sidB {
		t.Fatalf("winner %s", bound)
	}
	loser := sidA
	if bound == sidA {
		loser = sidB
	}
	wq, err := e.Get(ctx, bound)
	if err != nil {
		t.Fatal(err)
	}
	if wq["renderer_kind"] != RendererDiscord {
		t.Fatalf("winner kind %v", wq["renderer_kind"])
	}
	lq, err := e.Get(ctx, loser)
	if err != nil {
		t.Fatal(err)
	}
	if lq["renderer_kind"] == RendererDiscord {
		t.Fatal("loser still holds discord lease")
	}
}

func TestLosingBindCannotRevokeWinnerLease(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userA := seedUser(t, pool)
	userB := seedUser(t, pool)
	sidA, err := e.WebSession(ctx, userA, "a")
	if err != nil {
		t.Fatal(err)
	}
	sidB, err := e.WebSession(ctx, userB, "b")
	if err != nil {
		t.Fatal(err)
	}
	guild := "w1a-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	got, err := e.BindDiscordRenderer(ctx, guild, sidA, "ch", 0, "bot-a", 7)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.BindDiscordRenderer(ctx, guild, sidB, "ch", got.BindingRevision+99, "bot-b", 1)
	if !errors.Is(err, ErrBindConflict) {
		t.Fatalf("want bind_conflict got %v", err)
	}
	if err := e.ReleaseRenderer(ctx, sidA, RendererDiscord, "bot-a", 6); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stale release: %v", err)
	}
	if err := e.HeartbeatRenderer(ctx, sidA, RendererDiscord, "bot-a", 6); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stale heartbeat: %v", err)
	}
	q, err := e.Get(ctx, sidA)
	if err != nil {
		t.Fatal(err)
	}
	if q["renderer_kind"] != RendererDiscord {
		t.Fatalf("winner lease lost: %v", q["renderer_kind"])
	}
	var bound uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT session_id FROM discord_voice_runtime WHERE guild_id=$1`, guild).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if bound != sidA {
		t.Fatalf("runtime moved to %s", bound)
	}
}

func TestStaleGenerationCannotReleaseNewerLease(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	gen1, err := e.AcquireBrowserRenderer(ctx, sid, "client-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	gen2, err := e.AcquireBrowserRenderer(ctx, sid, "client-1", gen1)
	if err != nil {
		t.Fatal(err)
	}
	if gen2 <= gen1 {
		t.Fatalf("generation did not advance %d %d", gen1, gen2)
	}
	if err := e.ReleaseRenderer(ctx, sid, RendererBrowser, "client-1", gen1); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stale release: %v", err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["renderer_kind"] != RendererBrowser || genOf(q) != gen2 {
		t.Fatalf("lease %v gen %d want %d", q["renderer_kind"], genOf(q), gen2)
	}
}

func TestCommandReceiptsUniqueHashReplay(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	extra := map[string]any{"volume": 0.4, "command_id": "cmd-1"}
	if err := e.Control(ctx, sid, "volume", extra); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["volume"] != 0.4 {
		t.Fatalf("volume %v", q["volume"])
	}
	rev := revOf(q)
	if _, err := pool.Exec(ctx, `UPDATE playback_sessions SET volume=0.9 WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	if err := e.Control(ctx, sid, "volume", map[string]any{"volume": 0.4, "command_id": "cmd-1"}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["volume"] != 0.9 {
		t.Fatalf("replay mutated volume %v", q["volume"])
	}
	if revOf(q) != rev {
		t.Fatalf("replay bumped revision %d -> %d", rev, revOf(q))
	}
	err = e.Control(ctx, sid, "volume", map[string]any{"volume": 0.2, "command_id": "cmd-1"})
	if !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("hash conflict: %v", err)
	}
	rec, ok, err := e.GetCommandReceipt(ctx, sid, "cmd-1")
	if err != nil || !ok {
		t.Fatalf("receipt %v %v", ok, err)
	}
	if rec.ResultStatus != 200 || rec.RequestHash == "" {
		t.Fatalf("receipt %+v", rec)
	}
}

func TestPlayheadSequenceMonotonicResetsOnNewInstance(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a, b}, 0); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	inst := instanceOf(q)
	if inst == uuid.Nil || seqOf(q) != 1 {
		t.Fatalf("new instance seq=%d inst=%s", seqOf(q), inst)
	}
	if err := e.CheckpointPlayhead(ctx, sid, inst, 100); err != nil {
		t.Fatal(err)
	}
	if err := e.CheckpointPlayhead(ctx, sid, inst, 200); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if seqOf(q) != 3 {
		t.Fatalf("seq %d", seqOf(q))
	}
	if instanceOf(q) != inst {
		t.Fatal("checkpoint changed instance")
	}
	rev := revOf(q)
	if err := e.Control(ctx, sid, "pause", map[string]any{"position_ms": 250}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if seqOf(q) != 4 {
		t.Fatalf("pause should bump playhead_sequence, got %d", seqOf(q))
	}
	if instanceOf(q) != inst {
		t.Fatal("pause changed instance")
	}
	if err := e.Control(ctx, sid, "resume", nil); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if seqOf(q) != 4 {
		t.Fatalf("resume must not bump playhead_sequence, got %d", seqOf(q))
	}
	if err := e.Control(ctx, sid, "seek", map[string]any{"position_ms": 50}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if instanceOf(q) != inst {
		t.Fatal("pause/resume/seek changed instance")
	}
	if seqOf(q) != 5 {
		t.Fatalf("seek should bump playhead_sequence, got %d", seqOf(q))
	}
	if revOf(q) <= rev {
		t.Fatal("pause/resume/seek should bump state_revision")
	}
	if err := e.Control(ctx, sid, "skip", nil); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if instanceOf(q) == inst {
		t.Fatal("skip should mint new instance")
	}
	if seqOf(q) != 1 {
		t.Fatalf("new instance sequence %d want 1", seqOf(q))
	}
}

func TestSwitchRendererToBrowserKeepsRuntimeSession(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a, b}, 0); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	inst := instanceOf(q)
	guild := "w1a-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	br, err := e.BindDiscordRenderer(ctx, guild, sid, "ch", 0, "bot", 3)
	if err != nil {
		t.Fatal(err)
	}
	var bound uuid.UUID
	var bindRev int64
	if err := pool.QueryRow(ctx, `SELECT session_id, binding_revision FROM discord_voice_runtime WHERE guild_id=$1`, guild).Scan(&bound, &bindRev); err != nil {
		t.Fatal(err)
	}
	if bound != sid || bindRev != br.BindingRevision {
		t.Fatalf("runtime %s rev %d", bound, bindRev)
	}
	if err := e.SwitchRendererToBrowser(ctx, sid, "client-1", 4); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT session_id, binding_revision FROM discord_voice_runtime WHERE guild_id=$1`, guild).Scan(&bound, &bindRev); err != nil {
		t.Fatal(err)
	}
	if bound != sid {
		t.Fatalf("runtime.session_id changed to %s", bound)
	}
	if bindRev != br.BindingRevision {
		t.Fatalf("binding_revision changed %d -> %d", br.BindingRevision, bindRev)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["renderer_kind"] != RendererBrowser {
		t.Fatalf("kind %v", q["renderer_kind"])
	}
	if q["output_pref"] != OutputBrowser {
		t.Fatalf("output_pref %v", q["output_pref"])
	}
	if instanceOf(q) != inst {
		t.Fatal("renderer switch changed playback_instance_id")
	}
}

func TestCheckpointRejectsStaleInstance(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CheckpointPlayhead(ctx, sid, uuid.New(), 10); !errors.Is(err, ErrInstanceMismatch) {
		t.Fatalf("want instance_mismatch got %v", err)
	}
}
