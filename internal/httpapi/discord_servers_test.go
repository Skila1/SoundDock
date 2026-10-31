package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/audit"
	"github.com/sounddock/sounddock/internal/auth"
	discordx "github.com/sounddock/sounddock/internal/discord"
)

func TestKeptStart(t *testing.T) {
	orig := []string{"a", "b", "c", "d"}
	cases := []struct {
		kept  []string
		start int
		want  int
	}{
		{[]string{"a", "c", "d"}, 2, 1}, // same song
		{[]string{"a", "c", "d"}, 1, 1}, // dropped start moves to the next kept
		{[]string{"a", "b"}, 3, 0},      // nothing after start: from the top
		{[]string{"b", "c", "d"}, 0, 0},
	}
	for _, c := range cases {
		if got := keptStart(orig, c.kept, c.start); got != c.want {
			t.Errorf("kept=%v start=%d got %d want %d", c.kept, c.start, got, c.want)
		}
	}
}

// serverFixture is a server limited to library `allowed`, with one song in
// it, one in `hidden`, and a queue bound to the server.
type serverFixture struct {
	s                         *Server
	guild                     string
	allowed, hidden, admin    uuid.UUID
	allowedTrack, hiddenTrack uuid.UUID
	sid                       uuid.UUID
}

func newServerFixture(t *testing.T) serverFixture {
	pool := testPool(t)
	ctx := context.Background()
	f := serverFixture{s: &Server{Pool: pool, Audit: audit.New(pool)}, guild: "srv-" + uuid.NewString()[:8],
		allowed: uuid.New(), hidden: uuid.New(), admin: uuid.New(), allowedTrack: uuid.New(), hiddenTrack: uuid.New(), sid: uuid.New()}
	stor := uuid.New()
	tag := f.guild
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'local',$3)`, stor, tag, []byte("/tmp"))
	exec(`INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$4), ($3,$5,'music',$4)`, f.allowed, tag+"-a", f.hidden, stor, tag+"-h")
	exec(`INSERT INTO tracks (id, library_id, title, duration_ms) VALUES ($1,$2,'Allowed',1000), ($3,$4,'Hidden',1000)`, f.allowedTrack, f.allowed, f.hiddenTrack, f.hidden)
	exec(`INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x','Admin')`, f.admin, tag)
	exec(`INSERT INTO discord_guilds (id, name, library_mode, owner_id, owner_name) VALUES ($1,'Test server','selected','42','owner')`, f.guild)
	exec(`INSERT INTO discord_guild_libraries (guild_id, library_id) VALUES ($1,$2)`, f.guild, f.allowed)
	exec(`INSERT INTO playback_sessions (id, kind, owner_key) VALUES ($1,'discord_guild',$2)`, f.sid, f.guild)
	exec(`INSERT INTO discord_voice_runtime (guild_id, session_id, connected) VALUES ($1,$2,true)`, f.guild, f.sid)
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM audit_events WHERE meta->>'guild_id'=$1`, f.guild)
		_, _ = pool.Exec(c, `DELETE FROM discord_guilds WHERE id=$1`, f.guild)
		_, _ = pool.Exec(c, `DELETE FROM playback_sessions WHERE id=$1`, f.sid)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id = ANY($1)`, []uuid.UUID{f.allowedTrack, f.hiddenTrack})
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, f.admin)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id = ANY($1)`, []uuid.UUID{f.allowed, f.hidden})
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, stor)
	})
	return f
}

func TestServerTrackFilterDropsHiddenLibraries(t *testing.T) {
	f := newServerFixture(t)
	ctx := context.Background()
	refs := []string{f.hiddenTrack.String(), f.allowedTrack.String(), "yt:dQw4w9WgXcQ"}
	hints := []queueTrackHint{{ID: f.hiddenTrack.String(), Title: "Hidden"}, {ID: "yt:dQw4w9WgXcQ", Title: "From YouTube"}}
	kept, keptHints, skipped := f.s.serverTrackFilter(ctx, f.sid, refs, hints)
	if skipped != 1 || !slices.Equal(kept, []string{f.allowedTrack.String(), "yt:dQw4w9WgXcQ"}) {
		t.Fatalf("kept=%v skipped=%d", kept, skipped)
	}
	if len(keptHints) != 1 || keptHints[0].ID != "yt:dQw4w9WgXcQ" {
		t.Fatalf("hints %v", keptHints)
	}
	// A queue that is not bound to a server is never filtered.
	if _, _, n := f.s.serverTrackFilter(ctx, uuid.New(), refs, hints); n != 0 {
		t.Fatalf("unbound queue skipped %d", n)
	}
	// "All libraries" lifts the limit.
	if _, err := f.s.Pool.Exec(ctx, `UPDATE discord_guilds SET library_mode='all' WHERE id=$1`, f.guild); err != nil {
		t.Fatal(err)
	}
	if _, _, n := f.s.serverTrackFilter(ctx, f.sid, refs, hints); n != 0 {
		t.Fatalf("all libraries skipped %d", n)
	}
}

func TestLoadServerScope(t *testing.T) {
	f := newServerFixture(t)
	ctx := context.Background()
	sc := discordx.LoadServerScope(ctx, f.s.Pool, f.guild)
	if !sc.Restricted || sc.Banned || !sc.SharedQueue || !sc.Allows(f.allowed) || sc.Allows(f.hidden) {
		t.Fatalf("scope %+v", sc)
	}
	if libs := sc.SearchLibraries(); len(libs) != 1 || libs[0] != f.allowed {
		t.Fatalf("search libraries %v", libs)
	}
	if _, err := f.s.Pool.Exec(ctx, `DELETE FROM discord_guild_libraries WHERE guild_id=$1`, f.guild); err != nil {
		t.Fatal(err)
	}
	// No libraries picked must match nothing, not everything.
	if libs := discordx.LoadServerScope(ctx, f.s.Pool, f.guild).SearchLibraries(); len(libs) != 1 || libs[0] != uuid.Nil {
		t.Fatalf("empty selection %v", libs)
	}
	if sc := discordx.LoadServerScope(ctx, f.s.Pool, "unknown-"+f.guild); sc.Restricted || sc.SharedQueue || sc.Banned {
		t.Fatalf("unknown server %+v", sc)
	}
}

func TestNewServersDefaultToSharedQueue(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	id := "new-" + uuid.NewString()[:8]
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, id) })
	if _, err := pool.Exec(ctx, `INSERT INTO discord_guilds (id, name) VALUES ($1,'new')`, id); err != nil {
		t.Fatal(err)
	}
	if !discordx.LoadServerScope(ctx, pool, id).SharedQueue {
		t.Fatal("a new server should get one shared queue")
	}
}

func (f serverFixture) call(t *testing.T, h http.HandlerFunc, method, path string, body any) (int, map[string]any) {
	t.Helper()
	admin := &auth.User{ID: f.admin, Username: f.guild, IsAdmin: true}
	req := authedJSON(admin, method, path, body)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx("id", f.guild)))
	rec := httptest.NewRecorder()
	h(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAdminDiscordServerSettingsBanAndActivity(t *testing.T) {
	f := newServerFixture(t)
	base := "/api/v1/admin/discord/servers/" + f.guild

	code, got := f.call(t, f.s.adminDiscordServer, http.MethodGet, base, nil)
	if code != 200 || got["owner_name"] != "owner" || got["status"] != "connected" || got["library_mode"] != "selected" {
		t.Fatalf("get %d %v", code, got)
	}

	code, got = f.call(t, f.s.adminPatchDiscordServer, http.MethodPatch, base, map[string]any{
		"queue_mode": "shared", "library_ids": []string{f.allowed.String(), f.hidden.String()},
	})
	if code != 200 || got["queue_mode"] != "shared" {
		t.Fatalf("patch %d %v", code, got)
	}
	if ids, _ := got["library_ids"].([]any); len(ids) != 2 {
		t.Fatalf("library_ids %v", got["library_ids"])
	}
	if code, _ = f.call(t, f.s.adminPatchDiscordServer, http.MethodPatch, base, map[string]any{"queue_mode": "everyone"}); code != 400 {
		t.Fatalf("bad queue_mode got %d", code)
	}

	code, got = f.call(t, f.s.adminBanDiscordServer, http.MethodPost, base+"/ban", map[string]any{"reason": "spam"})
	if code != 200 || got["status"] != "banned" || got["ban_reason"] != "spam" || got["banned_by"] != "Admin" {
		t.Fatalf("ban %d %v", code, got)
	}
	if sc := discordx.LoadServerScope(context.Background(), f.s.Pool, f.guild); !sc.Banned {
		t.Fatal("scope not banned")
	}
	if f.s.guildPlaybackEnabled(context.Background(), f.guild) {
		t.Fatal("a banned server must not accept web playback")
	}

	code, got = f.call(t, f.s.adminUnbanDiscordServer, http.MethodPost, base+"/unban", nil)
	if code != 200 || got["status"] != "connected" {
		t.Fatalf("unban %d %v", code, got)
	}

	code, got = f.call(t, f.s.adminDiscordServerActivity, http.MethodGet, base+"/activity", nil)
	if code != 200 {
		t.Fatalf("activity %d %v", code, got)
	}
	items, _ := got["items"].([]any)
	var actions []string
	for _, it := range items {
		m := it.(map[string]any)
		actions = append(actions, m["action"].(string))
		if m["kind"] != "admin" || m["actor"] != "Admin" {
			t.Fatalf("item %v", m)
		}
	}
	want := []string{"discord.server.unban", "discord.server.ban", "discord.server.update"}
	if !slices.Equal(actions, want) {
		t.Fatalf("activity %v want %v", actions, want)
	}
}

func TestDescribePermissionsAdministratorGrantsAll(t *testing.T) {
	for _, p := range describePermissions(8) {
		if p["granted"] != true {
			t.Fatalf("%v not granted for an administrator", p["key"])
		}
	}
	got := map[string]bool{}
	for _, p := range describePermissions(1<<20 | 1<<21) {
		got[p["key"].(string)] = p["granted"].(bool)
	}
	if !got["connect"] || !got["speak"] || got["administrator"] || got["manage_server"] {
		t.Fatalf("voice-only bits %v", got)
	}
}
