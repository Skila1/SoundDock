package playback

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestEngineHasAudioListenerDB(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	sid, err := e.WebSession(ctx, userID, "listen-1")
	if err != nil {
		t.Fatal(err)
	}

	setLease := func(kind, status, rid string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			UPDATE playback_sessions SET renderer_kind=$2, status=$3, renderer_id=$4 WHERE id=$1`,
			sid, kind, status, rid); err != nil {
			t.Fatal(err)
		}
	}

	assert := func(name string, want bool) {
		t.Helper()
		got, err := e.HasAudioListener(ctx, sid)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != want {
			t.Fatalf("%s: got %v want %v", name, got, want)
		}
	}

	setLease("none", "stopped", "")
	assert("no listener", false)

	setLease("none", "playing", "")
	assert("sse presence only", false)

	setLease("browser", "playing", "tab-1")
	assert("browser rendering", true)

	setLease("browser", "paused", "tab-1")
	assert("browser paused", false)

	guild := "listen-" + uuid.NewString()[:8]
	ch := "ch-" + uuid.NewString()[:8]
	did := "u-" + uuid.NewString()[:8]
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

	setLease("discord", "playing", "bot-1")
	assert("discord renderer with zero humans", false)

	if _, err := pool.Exec(ctx, `
		INSERT INTO discord_user_voice (discord_user_id, guild_id, channel_id, updated_at)
		VALUES ($1,$2,$3,now())`, did, guild, ch); err != nil {
		t.Fatal(err)
	}
	assert("discord renderer plus human", true)

	setLease("discord", "playing", "")
	assert("discord humans without lease", false)
}
