package playback

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestBindGuildSessionNoGrantNoOpSameChannel(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	guild := "w2-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})

	first, err := e.BindGuildSession(ctx, guild, sid, "ch-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.BindingRevision < 1 {
		t.Fatalf("first bind revision %d", first.BindingRevision)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["renderer_kind"] == RendererDiscord {
		t.Fatal("bind must not grant a Discord lease")
	}
	rev := revOf(q)

	again, err := e.BindGuildSession(ctx, guild, sid, "ch-1", first.BindingRevision)
	if err != nil {
		t.Fatal(err)
	}
	if again.BindingRevision != first.BindingRevision {
		t.Fatalf("no-op bumped binding_revision %d -> %d", first.BindingRevision, again.BindingRevision)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if revOf(q) != rev {
		t.Fatalf("no-op bumped state_revision %d -> %d", rev, revOf(q))
	}
}

func TestClaimDiscordRendererNeverStealsBrowser(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.AcquireBrowserRenderer(ctx, sid, "tab-1", 0); err != nil {
		t.Fatal(err)
	}
	if err := e.ClaimDiscordRenderer(ctx, sid, "bot-1", 1, false); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stealBrowser=false must not take browser: %v", err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["renderer_kind"] != RendererBrowser {
		t.Fatalf("kind %v", q["renderer_kind"])
	}
	if err := e.ClaimDiscordRenderer(ctx, sid, "bot-1", 1, true); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["renderer_kind"] != RendererDiscord {
		t.Fatalf("stealBrowser=true should take browser, kind %v", q["renderer_kind"])
	}
	if q["output_pref"] != OutputDiscord {
		t.Fatalf("output_pref %v", q["output_pref"])
	}
}

func TestSeekPauseCheckpointResumeDoesNotBumpSeq(t *testing.T) {
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
	seq := seqOf(q)
	inst := instanceOf(q)

	if err := e.Control(ctx, sid, "seek", map[string]any{"position_ms": 1500}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if seqOf(q) != seq+1 {
		t.Fatalf("seek seq %d want %d", seqOf(q), seq+1)
	}
	if instanceOf(q) != inst {
		t.Fatal("seek changed instance")
	}
	pos, _ := q["position_ms"].(int)
	if pos != 1500 {
		t.Fatalf("seek position %v", q["position_ms"])
	}

	if err := e.Control(ctx, sid, "pause", map[string]any{"position_ms": 1800}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if seqOf(q) != seq+2 {
		t.Fatalf("pause seq %d want %d", seqOf(q), seq+2)
	}
	if q["status"] != "paused" {
		t.Fatalf("status %v", q["status"])
	}

	if err := e.Control(ctx, sid, "resume", nil); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if seqOf(q) != seq+2 {
		t.Fatalf("resume bumped seq to %d", seqOf(q))
	}
	if q["status"] != "playing" {
		t.Fatalf("resume status %v", q["status"])
	}
}

func TestRemoveCurrentMintsInstanceEmptyClears(t *testing.T) {
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
	if inst == uuid.Nil {
		t.Fatal("expected instance")
	}

	if err := e.Control(ctx, sid, "remove", map[string]any{"position": 0}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if instanceOf(q) == inst {
		t.Fatal("remove current must mint a new instance")
	}
	if instanceOf(q) == uuid.Nil {
		t.Fatal("remaining queue must have an instance")
	}
	pos, _ := q["position_ms"].(int)
	if pos != 0 {
		t.Fatalf("new instance position %v", q["position_ms"])
	}
	if seqOf(q) != 1 {
		t.Fatalf("new instance seq %d", seqOf(q))
	}

	if err := e.Control(ctx, sid, "remove", map[string]any{"position": 0}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["status"] != "stopped" {
		t.Fatalf("empty status %v", q["status"])
	}
	if instanceOf(q) != uuid.Nil {
		t.Fatalf("empty instance %s", instanceOf(q))
	}
	if seqOf(q) != 0 {
		t.Fatalf("empty seq %d", seqOf(q))
	}
}

func TestAddReceiptReplayVsIntentionalDuplicate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a}, 0); err != nil {
		t.Fatal(err)
	}

	extra := map[string]any{"track_ids": []uuid.UUID{b}, "next": false, "command_id": "add-1"}
	if err := e.Control(ctx, sid, "add", extra); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := q["items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("after add n=%d", len(items))
	}

	if err := e.Control(ctx, sid, "add", map[string]any{"track_ids": []uuid.UUID{b}, "next": false, "command_id": "add-1"}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	items, _ = q["items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("replay double-added n=%d", len(items))
	}

	if err := e.Control(ctx, sid, "add", map[string]any{"track_ids": []uuid.UUID{b}, "next": false, "command_id": "add-2"}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	items, _ = q["items"].([]map[string]any)
	if len(items) != 3 {
		t.Fatalf("intentional duplicate n=%d", len(items))
	}
}

func TestSetPositionSkipsWhenRendererOwned(t *testing.T) {
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
	if _, err := e.AcquireBrowserRenderer(ctx, sid, "tab-1", 0); err != nil {
		t.Fatal(err)
	}
	if err := e.Control(ctx, sid, "seek", map[string]any{"position_ms": 4000}); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPosition(ctx, sid, 99); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	pos, _ := q["position_ms"].(int)
	if pos != 4000 {
		t.Fatalf("listen SetPosition overwrote owner playhead: %v", q["position_ms"])
	}

	if err := e.ReleaseRenderer(ctx, sid, RendererBrowser, "tab-1", genOf(q)); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPosition(ctx, sid, 250); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	pos, _ = q["position_ms"].(int)
	if pos != 250 {
		t.Fatalf("unowned SetPosition %v", q["position_ms"])
	}
}

func TestRepeatOneEndedStaysOnControlSkip(t *testing.T) {
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
	if err := e.Control(ctx, sid, "repeat", map[string]any{"mode": "one"}); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	inst := instanceOf(q)
	cur := q["current_track_id"]

	if err := e.Control(ctx, sid, "skip", map[string]any{"ended": true}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if fmtTrack(q["current_track_id"]) != fmtTrack(cur) {
		t.Fatalf("ended skip left current %v want %v", q["current_track_id"], cur)
	}
	if instanceOf(q) == inst {
		t.Fatal("repeat-one ended still mints a new instance")
	}

	if err := e.Control(ctx, sid, "skip", nil); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if fmtTrack(q["current_track_id"]) == fmtTrack(cur) {
		t.Fatal("user skip with ended=false must leave repeat-one track")
	}
}

func fmtTrack(v any) string {
	switch t := v.(type) {
	case uuid.UUID:
		return t.String()
	case *uuid.UUID:
		if t != nil {
			return t.String()
		}
	}
	return ""
}

func TestPersistPositionKeepsRevisionAndIgnoresStaleInstance(t *testing.T) {
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
	if q["status"] != "playing" {
		t.Skipf("replace did not start playback: %v", q["status"])
	}
	rev, seq, inst := revOf(q), seqOf(q), instanceOf(q)
	ok, err := e.PersistPosition(ctx, sid, inst, 12000)
	if err != nil || !ok {
		t.Fatalf("persist: ok=%v err=%v", ok, err)
	}
	q, _ = e.Get(ctx, sid)
	if posOf(q) != 12000 || seqOf(q) != seq+1 {
		t.Fatalf("persist pos %d seq %d", posOf(q), seqOf(q))
	}
	if revOf(q) != rev {
		t.Fatalf("persist must not bump state_revision (undo stays valid): %d -> %d", rev, revOf(q))
	}
	if err := e.Control(ctx, sid, "skip", nil); err != nil {
		t.Fatal(err)
	}
	ok, err = e.PersistPosition(ctx, sid, inst, 180000)
	if err != nil || ok {
		t.Fatalf("a save for the previous track must be dropped: ok=%v err=%v", ok, err)
	}
	q, _ = e.Get(ctx, sid)
	if posOf(q) == 180000 {
		t.Fatal("stale save moved the next track")
	}
}

func TestResumeRestartsCheckpointClock(t *testing.T) {
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
	if err := e.Control(ctx, sid, "pause", map[string]any{"position_ms": 5000}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE playback_sessions SET checkpoint_at=now()-interval '1 hour' WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	if err := e.Control(ctx, sid, "resume", nil); err != nil {
		t.Fatal(err)
	}
	var age float64
	if err := pool.QueryRow(ctx, `SELECT extract(epoch FROM now()-checkpoint_at) FROM playback_sessions WHERE id=$1`, sid).Scan(&age); err != nil {
		t.Fatal(err)
	}
	if age > 60 {
		t.Fatalf("resume left checkpoint_at %0.fs in the past; observers add the paused span", age)
	}
	if _, err := pool.Exec(ctx, `UPDATE playback_sessions SET checkpoint_at=now()-interval '3 seconds' WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	if err := e.Control(ctx, sid, "resume", nil); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT extract(epoch FROM now()-checkpoint_at) FROM playback_sessions WHERE id=$1`, sid).Scan(&age); err != nil {
		t.Fatal(err)
	}
	if age < 2 {
		t.Fatal("resume while already playing must not restart the clock")
	}
}

// A session left bound in a guild the bot was kicked from reported that
// guild's higher binding_revision, and the browser rejected joins elsewhere.
func TestBindGuildSessionReleasesOtherGuilds(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	old, live := "w2o-"+uuid.NewString()[:8], "w2l-"+uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id = ANY($1)`, []string{old, live})
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id = ANY($1)`, []string{old, live})
	})
	if _, err := e.BindGuildSession(ctx, old, sid, "ch-old", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE discord_voice_runtime SET binding_revision=143, connected=true WHERE guild_id=$1`, old); err != nil {
		t.Fatal(err)
	}

	res, err := e.BindGuildSession(ctx, live, sid, "ch-live", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Released) != 1 || res.Released[0] != old {
		t.Fatalf("released %v, want [%s]", res.Released, old)
	}
	var bound *uuid.UUID
	var rev int64
	if err := pool.QueryRow(ctx, `SELECT session_id, binding_revision FROM discord_voice_runtime WHERE guild_id=$1`, old).Scan(&bound, &rev); err != nil {
		t.Fatal(err)
	}
	if bound != nil || rev != 144 {
		t.Fatalf("old guild still bound=%v rev=%d", bound, rev)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := q["binding_guild_id"].(*string); g == nil || *g != live {
		t.Fatalf("binding_guild_id %v, want %s", q["binding_guild_id"], live)
	}
	if r, _ := q["binding_revision"].(*int64); r == nil || *r != res.BindingRevision {
		t.Fatalf("binding_revision %v, want %d", q["binding_revision"], res.BindingRevision)
	}

	// Re-joining the same channel is a no-op bind but still clears a stray row.
	if _, err := pool.Exec(ctx, `UPDATE discord_voice_runtime SET session_id=$2 WHERE guild_id=$1`, old, sid); err != nil {
		t.Fatal(err)
	}
	again, err := e.BindGuildSession(ctx, live, sid, "ch-live", 0)
	if err != nil {
		t.Fatal(err)
	}
	if again.BindingRevision != res.BindingRevision {
		t.Fatalf("no-op bumped live revision %d -> %d", res.BindingRevision, again.BindingRevision)
	}
	if err := pool.QueryRow(ctx, `SELECT session_id FROM discord_voice_runtime WHERE guild_id=$1`, old).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if bound != nil {
		t.Fatal("no-op bind left the other guild bound")
	}
}
