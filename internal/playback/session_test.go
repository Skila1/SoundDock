package playback

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/testdb"
)

func testPool(t *testing.T) *pgxpool.Pool {
	return testdb.Open(t)
}

func TestWebSessionMigratesLegacyLeavesDiscord(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := uuid.New()
	username := "p1-" + userID.String()[:8]
	_, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_queue_items WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM party_votes WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1 OR owner_key LIKE $2)`, userID, userID.String()+"%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM party_members WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1 OR owner_key LIKE $2)`, userID, userID.String()+"%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE user_id=$1 OR owner_key=$2 OR owner_key LIKE $3`, userID, userID.String(), userID.String()+":%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE owner_key=$1 AND kind='discord_guild'`, "p1-guild-"+userID.String()[:8])
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	var webID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO playback_sessions (kind, owner_key, user_id) VALUES ('web_device',$1,$2) RETURNING id`, userID.String(), userID).Scan(&webID); err != nil {
		t.Fatal(err)
	}
	guildKey := "p1-guild-" + userID.String()[:8]
	var discID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO playback_sessions (kind, owner_key) VALUES ('discord_guild',$1) RETURNING id`, guildKey).Scan(&discID); err != nil {
		t.Fatal(err)
	}

	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if sid != webID {
		t.Fatalf("expected migrate same row %s got %s", webID, sid)
	}
	var kind, owner string
	var device *string
	if err := pool.QueryRow(ctx, `SELECT kind, owner_key, device_id FROM playback_sessions WHERE id=$1`, webID).Scan(&kind, &owner, &device); err != nil {
		t.Fatal(err)
	}
	if kind != "web_device" || owner != WebOwnerKey(userID, "browser-1") {
		t.Fatalf("web %s %s", kind, owner)
	}
	if device == nil || *device != "browser-1" {
		t.Fatalf("device %v", device)
	}

	sid2, err := e.WebSession(ctx, userID, "browser-2")
	if err != nil {
		t.Fatal(err)
	}
	if sid2 == webID {
		t.Fatal("second device must be a new row")
	}

	gotDisc, err := e.Session(ctx, "discord_guild", guildKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotDisc != discID {
		t.Fatalf("discord id %s %s", gotDisc, discID)
	}
	if err := pool.QueryRow(ctx, `SELECT kind, owner_key FROM playback_sessions WHERE id=$1`, discID).Scan(&kind, &owner); err != nil {
		t.Fatal(err)
	}
	if kind != "discord_guild" || owner != guildKey {
		t.Fatalf("discord mutated %s %s", kind, owner)
	}
}

func TestQueueGetAdditiveFields(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := uuid.New()
	username := "p1q-" + userID.String()[:8]
	_, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_queue_items WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "kind", "owner_key", "volume", "repeat", "shuffle", "crossfade_seconds", "replaygain_mode", "current_index", "current_track_id", "position_ms", "status", "items", "shuffle_mode", "stop_after_current", "device_id", "state_revision", "playhead_sequence", "playback_instance_id", "muted", "output_pref", "autoplay", "renderer_kind", "renderer_id", "renderer_generation"} {
		if _, ok := q[k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
	if q["kind"] != "web_device" {
		t.Fatalf("kind %v", q["kind"])
	}
	if q["owner_key"] != WebOwnerKey(userID, "browser-1") {
		t.Fatalf("owner %v", q["owner_key"])
	}
	if q["shuffle_mode"] != "random" {
		t.Fatalf("shuffle_mode %v", q["shuffle_mode"])
	}
}

func TestControlStopAfterCurrentExtraKeys(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := uuid.New()
	username := "p1s-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Control(ctx, sid, "stop_after_current", map[string]any{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["stop_after_current"] != true {
		t.Fatalf("enabled got %v", q["stop_after_current"])
	}
	if err := e.Control(ctx, sid, "stop_after_current", map[string]any{"enabled": false}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["stop_after_current"] != false {
		t.Fatalf("disabled got %v", q["stop_after_current"])
	}
	if err := e.Control(ctx, sid, "pause", map[string]any{"stop_after_current": true}); err != nil {
		t.Fatal(err)
	}
	q, err = e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["stop_after_current"] != true {
		t.Fatalf("extra field got %v", q["stop_after_current"])
	}
}

func TestExpirePayloadJSON(t *testing.T) {
	var p ExpirePayload
	if err := json.Unmarshal([]byte(`{"session_id":"00000000-0000-4000-8000-000000000070"}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.SessionID != uuid.MustParse("00000000-0000-4000-8000-000000000070") {
		t.Fatalf("session_id %s", p.SessionID)
	}
}

func mustTime(s string) time.Time {
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return tm
}
