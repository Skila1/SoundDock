package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/playback"
)

func wave1HTTPServer(t *testing.T) (*Server, *pgxpool.Pool) {
	t.Helper()
	pool := testPool(t)
	return &Server{Pool: pool, Play: playback.New(pool)}, pool
}

func seedQueueUser(t *testing.T, pool *pgxpool.Pool, discordID string) *auth.User {
	t.Helper()
	u := &auth.User{ID: uuid.New(), Username: "w1b-" + uuid.NewString()[:8], DisplayName: "w1b"}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$3)`, u.ID, u.Username, u.DisplayName); err != nil {
		t.Skip(err)
	}
	if discordID != "" {
		if _, err := pool.Exec(ctx, `INSERT INTO user_identities (user_id, provider, provider_user_id) VALUES ($1,'discord',$2)`, u.ID, discordID); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM playback_command_receipts WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, u.ID)
		_, _ = pool.Exec(c, `DELETE FROM discord_voice_runtime WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, u.ID)
		_, _ = pool.Exec(c, `DELETE FROM playback_queue_items WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, u.ID)
		_, _ = pool.Exec(c, `DELETE FROM user_identities WHERE user_id=$1`, u.ID)
		_, _ = pool.Exec(c, `DELETE FROM playback_sessions WHERE user_id=$1`, u.ID)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, u.ID)
	})
	return u
}

func putUserVoice(t *testing.T, pool *pgxpool.Pool, discordID, guildID, channelID string) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `INSERT INTO discord_guilds (id) VALUES ($1) ON CONFLICT DO NOTHING`, guildID)
	if _, err := pool.Exec(ctx, `
		INSERT INTO discord_user_voice (discord_user_id, guild_id, channel_id, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (discord_user_id, guild_id) DO UPDATE SET channel_id=EXCLUDED.channel_id, updated_at=now()`,
		discordID, guildID, channelID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_user_voice WHERE discord_user_id=$1`, discordID)
	})
}

func authedJSON(u *auth.User, method, path string, body any) *http.Request {
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	return r.WithContext(context.WithValue(r.Context(), userKey, u))
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json %v body %s", err, rec.Body.String())
	}
	return out
}

func queueID(m map[string]any) string {
	switch v := m["id"].(type) {
	case string:
		return v
	default:
		return ""
	}
}

func TestUnlinkedUserNeverAttaches(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	guest := seedQueueUser(t, pool, "")
	rec := httptest.NewRecorder()
	s.getQueue(rec, authedJSON(guest, http.MethodGet, "/api/v1/me/queue", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	if got["kind"] != "web_device" {
		t.Fatalf("kind %v", got["kind"])
	}
	if _, ok := got["state_revision"]; !ok {
		t.Fatal("missing state_revision")
	}
}

func TestGuestInBoundVCAttachesHostSession(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	didHost := "w1b-h-" + uuid.NewString()[:8]
	didGuest := "w1b-g-" + uuid.NewString()[:8]
	host := seedQueueUser(t, pool, didHost)
	guest := seedQueueUser(t, pool, didGuest)
	guild := "w1b-" + uuid.NewString()[:8]
	ch := "w1b-ch-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM discord_user_voice WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	putUserVoice(t, pool, didHost, guild, ch)
	putUserVoice(t, pool, didGuest, guild, ch)

	rec := httptest.NewRecorder()
	s.discordJoin(rec, authedJSON(host, http.MethodPost, "/api/v1/me/discord/join", map[string]any{}))
	if rec.Code != 200 {
		t.Fatalf("join %d %s", rec.Code, rec.Body.String())
	}
	join := decodeMap(t, rec)
	hostSID, _ := join["session_id"].(string)
	if hostSID == "" {
		t.Fatal("join session_id")
	}
	if _, ok := join["binding_revision"]; !ok {
		t.Fatal("join missing binding_revision")
	}
	if join["binding_revision"].(float64) < 1 {
		t.Fatalf("binding_revision %v", join["binding_revision"])
	}

	grec := httptest.NewRecorder()
	s.getQueue(grec, authedJSON(guest, http.MethodGet, "/api/v1/me/queue", nil))
	if grec.Code != 200 {
		t.Fatalf("guest get %d %s", grec.Code, grec.Body.String())
	}
	gq := decodeMap(t, grec)
	if queueID(gq) != hostSID {
		t.Fatalf("guest attached %s want host %s", queueID(gq), hostSID)
	}

	_, _ = pool.Exec(context.Background(), `UPDATE discord_user_voice SET channel_id=NULL WHERE discord_user_id=$1`, didGuest)
	left := httptest.NewRecorder()
	s.getQueue(left, authedJSON(guest, http.MethodGet, "/api/v1/me/queue", nil))
	if left.Code != 200 {
		t.Fatalf("left get %d %s", left.Code, left.Body.String())
	}
	lq := decodeMap(t, left)
	if queueID(lq) == hostSID {
		t.Fatal("guest still attached after leave-VC")
	}
	if lq["kind"] != "web_device" {
		t.Fatalf("personal kind %v", lq["kind"])
	}
}

func TestTargetDiscordIsNotSecondQueue(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	u := seedQueueUser(t, pool, "")
	rec := httptest.NewRecorder()
	s.getQueue(rec, authedJSON(u, http.MethodGet, "/api/v1/me/queue?target=discord", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d %s want personal session not 409", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	if got["kind"] != "web_device" {
		t.Fatalf("kind %v", got["kind"])
	}
}

func TestDiscordJoinBindsAttachedSession(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	did := "w1b-j-" + uuid.NewString()[:8]
	u := seedQueueUser(t, pool, did)
	guild := "w1b-jg-" + uuid.NewString()[:8]
	ch := "w1b-jc-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM discord_user_voice WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM playback_sessions WHERE kind='discord_guild' AND owner_key=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	putUserVoice(t, pool, did, guild, ch)
	rec := httptest.NewRecorder()
	s.discordJoin(rec, authedJSON(u, http.MethodPost, "/api/v1/me/discord/join", map[string]any{}))
	if rec.Code != 200 {
		t.Fatalf("join %d %s", rec.Code, rec.Body.String())
	}
	join := decodeMap(t, rec)
	sid := join["session_id"].(string)
	var runtimeSID uuid.UUID
	var rendererID *string
	var kind string
	if err := pool.QueryRow(context.Background(), `
		SELECT r.session_id, s.kind, s.renderer_id
		FROM discord_voice_runtime r JOIN playback_sessions s ON s.id=r.session_id
		WHERE r.guild_id=$1`, guild).Scan(&runtimeSID, &kind, &rendererID); err != nil {
		t.Fatal(err)
	}
	if runtimeSID.String() != sid {
		t.Fatalf("runtime %s join %s", runtimeSID, sid)
	}
	if kind != "web_device" {
		t.Fatalf("bound kind %s (must not be discord_guild)", kind)
	}
	if rendererID != nil && *rendererID != "" {
		t.Fatalf("join must not grant a renderer, renderer_id=%v", rendererID)
	}

	stale := httptest.NewRecorder()
	s.discordJoin(stale, authedJSON(u, http.MethodPost, "/api/v1/me/discord/join", map[string]any{"expected_binding_revision": 99}))
	if stale.Code != 409 {
		t.Fatalf("stale bind %d %s", stale.Code, stale.Body.String())
	}
	if !bytes.Contains(stale.Body.Bytes(), []byte("bind_conflict")) {
		t.Fatalf("want bind_conflict %s", stale.Body.String())
	}
}

func TestDiscordPlayDoesNotCopyGuildQueue(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	did := "w1b-p-" + uuid.NewString()[:8]
	u := seedQueueUser(t, pool, did)
	guild := "w1b-pg-" + uuid.NewString()[:8]
	ch := "w1b-pc-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id IN (SELECT id FROM playback_sessions WHERE owner_key=$1)`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_user_voice WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM playback_sessions WHERE kind='discord_guild' AND owner_key=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	putUserVoice(t, pool, did, guild, ch)
	guildSID, err := s.Play.Session(context.Background(), "discord_guild", guild, nil)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.discordPlay(rec, authedJSON(u, http.MethodPost, "/api/v1/me/discord/play", map[string]any{}))
	if rec.Code != 200 {
		t.Fatalf("play %d %s", rec.Code, rec.Body.String())
	}
	out := decodeMap(t, rec)
	attached := out["session_id"].(string)
	if attached == guildSID.String() {
		t.Fatal("play still used discord_guild session")
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM playback_queue_items WHERE session_id=$1`, guildSID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("guild queue copied n=%d", n)
	}
}

func TestSwitchRendererToBrowserKeepsBind(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	did := "w1b-s-" + uuid.NewString()[:8]
	u := seedQueueUser(t, pool, did)
	guild := "w1b-sg-" + uuid.NewString()[:8]
	ch := "w1b-sc-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM discord_user_voice WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	putUserVoice(t, pool, did, guild, ch)
	jrec := httptest.NewRecorder()
	s.discordJoin(jrec, authedJSON(u, http.MethodPost, "/api/v1/me/discord/join", map[string]any{}))
	if jrec.Code != 200 {
		t.Fatalf("join %d %s", jrec.Code, jrec.Body.String())
	}
	sid := decodeMap(t, jrec)["session_id"].(string)

	crec := httptest.NewRecorder()
	s.queueControl(crec, authedJSON(u, http.MethodPost, "/api/v1/me/queue/control", map[string]any{
		"action": "output_pref",
		"extra":  map[string]any{"output_pref": "browser", "renderer_id": "http-browser"},
	}))
	if crec.Code != 200 {
		t.Fatalf("switch %d %s", crec.Code, crec.Body.String())
	}
	got := decodeMap(t, crec)
	if got["renderer_kind"] != playback.RendererBrowser {
		t.Fatalf("renderer_kind %v", got["renderer_kind"])
	}
	var bound uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT session_id FROM discord_voice_runtime WHERE guild_id=$1`, guild).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if bound.String() != sid {
		t.Fatalf("unbind stole session %s want %s", bound, sid)
	}
}

func TestControlCommandConflict409(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	u := seedQueueUser(t, pool, "")
	first := httptest.NewRecorder()
	s.queueControl(first, authedJSON(u, http.MethodPost, "/api/v1/me/queue/control", map[string]any{
		"action":     "volume",
		"extra":      map[string]any{"volume": 0.4, "command_id": "cmd-w1b"},
		"command_id": "cmd-w1b",
	}))
	if first.Code != 200 {
		t.Fatalf("first %d %s", first.Code, first.Body.String())
	}
	conflict := httptest.NewRecorder()
	s.queueControl(conflict, authedJSON(u, http.MethodPost, "/api/v1/me/queue/control", map[string]any{
		"action": "volume",
		"extra":  map[string]any{"volume": 0.2, "command_id": "cmd-w1b"},
	}))
	if conflict.Code != 409 {
		t.Fatalf("conflict %d %s", conflict.Code, conflict.Body.String())
	}
	if !bytes.Contains(conflict.Body.Bytes(), []byte("command_conflict")) {
		t.Fatalf("body %s", conflict.Body.String())
	}
}

func TestAcquireBrowserLeaseStealsDiscordOnUserGesture(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	did := "w1b-a-" + uuid.NewString()[:8]
	u := seedQueueUser(t, pool, did)
	guild := "w1b-ag-" + uuid.NewString()[:8]
	ch := "w1b-ac-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM discord_user_voice WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	putUserVoice(t, pool, did, guild, ch)
	jrec := httptest.NewRecorder()
	s.discordJoin(jrec, authedJSON(u, http.MethodPost, "/api/v1/me/discord/join", map[string]any{}))
	if jrec.Code != 200 {
		t.Fatalf("join %d %s", jrec.Code, jrec.Body.String())
	}
	sidStr, _ := decodeMap(t, jrec)["session_id"].(string)
	sid, err := uuid.Parse(sidStr)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Play.ClaimDiscordRenderer(context.Background(), sid, "bot-1", 1, true); err != nil {
		t.Fatal(err)
	}
	arec := httptest.NewRecorder()
	s.queueRendererAcquire(arec, authedJSON(u, http.MethodPost, "/api/v1/me/queue/renderer/acquire", map[string]any{
		"renderer_id": "browser-1",
	}))
	if arec.Code != 200 {
		t.Fatalf("acquire %d %s", arec.Code, arec.Body.String())
	}
}

func TestUnlinkedDiscordListenerUsesVoiceDisplayName(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	didHost := "w1b-hn-" + uuid.NewString()[:8]
	didGuest := "w1b-gn-" + uuid.NewString()[:8]
	host := seedQueueUser(t, pool, didHost)
	guild := "w1b-ng-" + uuid.NewString()[:8]
	ch := "w1b-nc-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM discord_user_voice WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	putUserVoice(t, pool, didHost, guild, ch)
	putUserVoice(t, pool, didGuest, guild, ch)
	if _, err := pool.Exec(context.Background(), `UPDATE discord_user_voice SET display_name=$2 WHERE discord_user_id=$1`, didGuest, "Pixel"); err != nil {
		t.Fatal(err)
	}
	jrec := httptest.NewRecorder()
	s.discordJoin(jrec, authedJSON(host, http.MethodPost, "/api/v1/me/discord/join", map[string]any{}))
	if jrec.Code != 200 {
		t.Fatalf("join %d %s", jrec.Code, jrec.Body.String())
	}
	rec := httptest.NewRecorder()
	s.getQueue(rec, authedJSON(host, http.MethodGet, "/api/v1/me/queue", nil))
	if rec.Code != 200 {
		t.Fatalf("queue %d %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	raw, _ := got["listeners"].([]any)
	var names []string
	for _, item := range raw {
		row, _ := item.(map[string]any)
		if n, _ := row["display_name"].(string); n != "" {
			names = append(names, n)
		}
	}
	found := false
	for _, n := range names {
		if n == "Pixel" {
			found = true
		}
		if n == didGuest {
			t.Fatalf("unlinked listener used Discord id %q names=%v", didGuest, names)
		}
	}
	if !found {
		t.Fatalf("want Pixel in listeners, got %v", names)
	}
	for _, item := range raw {
		row, _ := item.(map[string]any)
		if row["display_name"] == "Pixel" && row["user_id"] != didGuest {
			t.Fatalf("unlinked listener user_id %v want %s", row["user_id"], didGuest)
		}
	}
}

func TestDisconnectedRuntimeDoesNotAttach(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	didHost := "w1b-dh-" + uuid.NewString()[:8]
	didGuest := "w1b-dg-" + uuid.NewString()[:8]
	host := seedQueueUser(t, pool, didHost)
	guest := seedQueueUser(t, pool, didGuest)
	guild := "w1b-dgx-" + uuid.NewString()[:8]
	ch := "w1b-dgc-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM discord_user_voice WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(ctx, `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	putUserVoice(t, pool, didHost, guild, ch)
	putUserVoice(t, pool, didGuest, guild, ch)
	jrec := httptest.NewRecorder()
	s.discordJoin(jrec, authedJSON(host, http.MethodPost, "/api/v1/me/discord/join", map[string]any{}))
	if jrec.Code != 200 {
		t.Fatalf("join %d %s", jrec.Code, jrec.Body.String())
	}
	hostSID, _ := decodeMap(t, jrec)["session_id"].(string)
	_, _ = pool.Exec(context.Background(), `
		UPDATE discord_voice_runtime SET connected=false, last_disconnect_reason='leave' WHERE guild_id=$1`, guild)
	grec := httptest.NewRecorder()
	s.getQueue(grec, authedJSON(guest, http.MethodGet, "/api/v1/me/queue", nil))
	if grec.Code != 200 {
		t.Fatalf("guest get %d %s", grec.Code, grec.Body.String())
	}
	gq := decodeMap(t, grec)
	if queueID(gq) == hostSID {
		t.Fatal("stale disconnected runtime still attached the guest")
	}
	if gq["kind"] != "web_device" {
		t.Fatalf("kind %v", gq["kind"])
	}
}

func TestQueueGetIncludesEngineFields(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	u := seedQueueUser(t, pool, "")
	rec := httptest.NewRecorder()
	s.getQueue(rec, authedJSON(u, http.MethodGet, "/api/v1/me/queue", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	for _, k := range []string{
		"state_revision", "playhead_sequence", "playback_instance_id", "muted",
		"output_pref", "autoplay", "renderer_kind", "renderer_id", "renderer_generation",
		"checkpoint_at", "duration_ms", "playback_rate", "binding_revision",
	} {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
}
