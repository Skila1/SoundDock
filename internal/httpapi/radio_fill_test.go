package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/radio"
)

func TestRadioFillYouTubeListenerGate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool, Play: playback.New(pool)}
	called := 0
	s.youtubeFillHook = func(context.Context, uuid.UUID, int, []uuid.UUID) []string {
		called++
		return []string{"yt-fill-1"}
	}

	u := seedQueueUser(t, pool, "")
	sid, err := s.Play.WebSession(ctx, u.ID, "web")
	if err != nil {
		t.Fatal(err)
	}
	fix := seedGrantLibs(t, pool)

	get := func() (map[string]any, int) {
		t.Helper()
		called = 0
		path := "/api/v1/radio?kind=track&fill=youtube&limit=50&seed_id=" + fix.trackID.String()
		req := authedJSON(u, http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.getRadio(rec, req)
		var body map[string]any
		if rec.Code == 200 {
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("json %v %s", err, rec.Body.String())
			}
		}
		return body, rec.Code
	}

	if radio.ClampFill(50) != 10 {
		t.Fatal("ClampFill must cap youtube fill at 10")
	}

	if _, err := pool.Exec(ctx, `
		UPDATE playback_sessions SET renderer_kind='none', status='playing', renderer_id='' WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	body, code := get()
	if code != 200 {
		t.Fatalf("sse-only status %d %s", code, body)
	}
	if called != 0 {
		t.Fatal("SSE presence must not call YouTube fill")
	}
	if _, ok := body["youtube_ids"]; ok && body["youtube_ids"] != nil {
		if ids, _ := body["youtube_ids"].([]any); len(ids) > 0 {
			t.Fatalf("youtube_ids %v", body["youtube_ids"])
		}
	}

	if _, err := pool.Exec(ctx, `
		UPDATE playback_sessions SET renderer_kind='browser', status='playing', renderer_id='tab-1' WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	body, code = get()
	if code != 200 {
		t.Fatalf("browser status %d %s", code, body)
	}
	if called != 1 {
		t.Fatalf("browser renderer should fill, calls=%d", called)
	}
	ids, _ := body["youtube_ids"].([]any)
	if len(ids) != 1 || ids[0] != "yt-fill-1" {
		t.Fatalf("youtube_ids %v", body["youtube_ids"])
	}

	if _, err := pool.Exec(ctx, `
		UPDATE playback_sessions SET renderer_kind='discord', status='playing', renderer_id='bot-1' WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	body, code = get()
	if code != 200 {
		t.Fatalf("discord empty vc %d", code)
	}
	if called != 0 {
		t.Fatal("discord with zero humans must not fill")
	}

	guild := "rf-" + uuid.NewString()[:8]
	ch := "ch-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM discord_user_voice WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(c, `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(c, `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO discord_guilds (id) VALUES ($1)`, guild); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO discord_voice_runtime (guild_id, voice_channel_id, session_id, connected)
		VALUES ($1,$2,$3,true)`, guild, ch, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO discord_user_voice (discord_user_id, guild_id, channel_id, updated_at)
		VALUES ($1,$2,$3,now())`, "rf-u-"+uuid.NewString()[:8], guild, ch); err != nil {
		t.Fatal(err)
	}
	body, code = get()
	if code != 200 {
		t.Fatalf("discord+human %d", code)
	}
	if called != 1 {
		t.Fatalf("discord renderer + human should fill, calls=%d", called)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE playback_sessions SET renderer_kind='none', status='stopped', renderer_id='' WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	_, code = get()
	if code != 200 {
		t.Fatalf("no listener %d", code)
	}
	if called != 0 {
		t.Fatal("no listener must not fill")
	}
}
