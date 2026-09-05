package discordx

import (
	"context"
	"errors"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/testdb"
)

func TestNewAssignsCryptoRendererIdentity(t *testing.T) {
	a := New(nil, nil, nil, nil, nil, nil)
	b := New(nil, nil, nil, nil, nil, nil)
	if a.rendererID == "" || b.rendererID == "" {
		t.Fatal("renderer_id must be set on New")
	}
	if a.rendererID == b.rendererID {
		t.Fatal("each process identity must be unique")
	}
	if _, err := uuid.Parse(a.rendererID); err != nil {
		t.Fatalf("renderer_id must be a UUID: %v", err)
	}
	if a.generation != 1 || b.generation != 1 {
		t.Fatalf("generation=%d/%d want 1", a.generation, b.generation)
	}
}

func TestResetRendererIdentityChangesIDKeepsGenerationOne(t *testing.T) {
	bot := New(nil, nil, nil, nil, nil, nil)
	old := bot.rendererID
	bot.resetRendererIdentity()
	if bot.rendererID == "" || bot.rendererID == old {
		t.Fatal("Run/reset must mint a new renderer_id")
	}
	if bot.generation != 1 {
		t.Fatalf("generation=%d want 1", bot.generation)
	}
}

func TestResumeOnRestartDefaultsFalse(t *testing.T) {
	t.Setenv("SD_DISCORD_RESUME_ON_RESTART", "")
	bot := New(nil, nil, nil, nil, nil, nil)
	if bot.resumeOnRestart {
		t.Fatal("resume_on_restart must default false")
	}
}

func TestShouldPauseAfterReclaimOnlyStaleDiscordPlaying(t *testing.T) {
	if !shouldPauseAfterReclaim(false, playback.RendererDiscord, "old", 1, "new", 1, "playing") {
		t.Fatal("stale discord worker reclaim of playing session should pause")
	}
	if shouldPauseAfterReclaim(true, playback.RendererDiscord, "old", 1, "new", 1, "playing") {
		t.Fatal("resume_on_restart must allow auto-play after reclaim")
	}
	if shouldPauseAfterReclaim(false, playback.RendererNone, "", 0, "new", 1, "playing") {
		t.Fatal("first grant from none must not pause HTTP/play")
	}
	if shouldPauseAfterReclaim(false, playback.RendererDiscord, "old", 1, "new", 1, "paused") {
		t.Fatal("already paused stays paused")
	}
	if shouldPauseAfterReclaim(false, playback.RendererDiscord, "same", 1, "same", 1, "playing") {
		t.Fatal("same identity is not a reclaim")
	}
}

func TestShouldClaimDiscordLeaseNeverStealsBrowser(t *testing.T) {
	if shouldClaimDiscordLease("web_device", playback.OutputDiscord, playback.RendererBrowser) {
		t.Fatal("must never steal a live browser tab")
	}
	if !shouldClaimDiscordLease("web_device", playback.OutputBrowser, playback.RendererNone) {
		t.Fatal("empty holder must claim so Discord works with the web tab closed")
	}
	if !shouldClaimDiscordLease("web_device", playback.OutputDiscord, playback.RendererNone) {
		t.Fatal("explicit discord output on an empty holder may claim")
	}
	if !shouldClaimDiscordLease("discord_guild", playback.OutputBrowser, playback.RendererNone) {
		t.Fatal("guild-native session may claim despite schema default pref")
	}
	if !shouldEmitDiscordPCM(map[string]any{
		"renderer_kind": playback.RendererDiscord, "renderer_id": "bot", "renderer_generation": int64(1),
		"output_pref": playback.OutputBrowser,
	}, "bot", 1) {
		t.Fatal("held Discord lease must emit even if leftover web pref is browser")
	}
	if !shouldEmitDiscordPCM(map[string]any{
		"renderer_kind": playback.RendererDiscord, "renderer_id": "bot", "renderer_generation": int64(1),
		"output_pref": playback.OutputDiscord,
	}, "bot", 1) {
		t.Fatal("held discord + output_pref=discord must emit")
	}
}

func TestHoldsRendererLeaseMatchesIdentity(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	st := map[string]any{
		"renderer_kind":       playback.RendererDiscord,
		"renderer_id":         id,
		"renderer_generation": int64(1),
	}
	if !holdsRendererLease(st, id, 1) {
		t.Fatal("matching identity should hold")
	}
	if holdsRendererLease(st, id, 2) {
		t.Fatal("old generation must not hold the new lease")
	}
	if holdsRendererLease(st, "other", 1) {
		t.Fatal("other renderer_id must not hold")
	}
	ptr := id
	st["renderer_id"] = &ptr
	if !holdsRendererLease(st, id, 1) {
		t.Fatal("*string renderer_id")
	}
}

func TestPlaybackInstanceID(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000099")
	if playbackInstanceID(nil) != uuid.Nil {
		t.Fatal("nil")
	}
	if playbackInstanceID(map[string]any{"playback_instance_id": id}) != id {
		t.Fatal("uuid")
	}
	if playbackInstanceID(map[string]any{"playback_instance_id": &id}) != id {
		t.Fatal("*uuid")
	}
}

func TestExtraWithCommandIDUsesInteractionSnowflake(t *testing.T) {
	got := extraWithCommandID(map[string]any{"volume": 0.4}, "123456789012345678")
	if got["command_id"] != "123456789012345678" {
		t.Fatalf("command_id=%v", got["command_id"])
	}
	if got["volume"] != 0.4 {
		t.Fatal("other extra keys must be preserved")
	}
	if extraWithCommandID(nil, "1")["command_id"] != "1" {
		t.Fatal("nil extra")
	}
}

func TestMutatingSlashCommands(t *testing.T) {
	for _, name := range []string{"pause", "resume", "skip", "previous", "stop", "clear", "shuffle", "repeat", "volume"} {
		if !mutatingSlashCommand(name) {
			t.Fatalf("%s should defer+command_id", name)
		}
	}
	if mutatingSlashCommand("queue") || mutatingSlashCommand("join") {
		t.Fatal("reads/join are not mutating receipts")
	}
}

func TestInteractionCommandIDIsSnowflake(t *testing.T) {
	i := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{ID: "9876543210"}}
	if extraWithCommandID(nil, i.ID)["command_id"] != "9876543210" {
		t.Fatal("slash command_id must be i.ID")
	}
}

func discordTestPool(t *testing.T) *pgxpool.Pool {
	return testdb.Open(t)
}

func TestEnsureBoundSessionUsesRuntimeNotFreshGuildRow(t *testing.T) {
	pool := discordTestPool(t)
	ctx := context.Background()
	play := playback.New(pool)
	guild := "w1b-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_queue_items WHERE session_id IN (SELECT id FROM playback_sessions WHERE kind='discord_guild' AND owner_key=$1)`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_command_receipts WHERE session_id IN (SELECT id FROM playback_sessions WHERE kind='discord_guild' AND owner_key=$1)`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE kind='discord_guild' AND owner_key=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})

	native, err := play.Session(ctx, "discord_guild", guild, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := play.Session(ctx, "web_device", "w1b-other-"+guild, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := play.BindDiscordRenderer(ctx, guild, other, "vc-1", 0, "other-bot", 1); err != nil {
		t.Fatal(err)
	}

	bot := New(pool, nil, nil, play, nil, nil)
	sid, err := bot.ensureBoundSession(ctx, guild, "vc-1")
	if err != nil {
		t.Fatal(err)
	}
	if sid != other {
		t.Fatalf("bound session %s want runtime session %s (not guild-native %s)", sid, other, native)
	}
	st, err := play.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !holdsRendererLease(st, bot.rendererID, bot.generation) {
		t.Fatalf("worker must CAS-reclaim lease, kind=%v id=%v gen=%v", st["renderer_kind"], st["renderer_id"], st["renderer_generation"])
	}
}

func TestStaleGenerationCannotHeartbeatNewProcess(t *testing.T) {
	pool := discordTestPool(t)
	ctx := context.Background()
	play := playback.New(pool)
	guild := "w1b-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE kind='discord_guild' AND owner_key=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	oldBot := New(pool, nil, nil, play, nil, nil)
	sid, err := oldBot.ensureBoundSession(ctx, guild, "vc")
	if err != nil {
		t.Fatal(err)
	}
	newBot := New(pool, nil, nil, play, nil, nil)
	if _, err := newBot.ensureBoundSession(ctx, guild, "vc"); err != nil {
		t.Fatal(err)
	}
	if err := oldBot.heartbeatLease(ctx, sid); !errors.Is(err, playback.ErrLeaseConflict) {
		t.Fatalf("old process heartbeat: %v", err)
	}
}

func TestHeartbeatDoesNotBumpStateRevision(t *testing.T) {
	pool := discordTestPool(t)
	ctx := context.Background()
	play := playback.New(pool)
	guild := "w1b-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE kind='discord_guild' AND owner_key=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	bot := New(pool, nil, nil, play, nil, nil)
	sid, err := bot.ensureBoundSession(ctx, guild, "vc")
	if err != nil {
		t.Fatal(err)
	}
	before, err := play.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	rev := int64Of(before, "state_revision")
	seq := int64Of(before, "playhead_sequence")
	if err := bot.heartbeatLease(ctx, sid); err != nil {
		t.Fatal(err)
	}
	after, err := play.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if int64Of(after, "state_revision") != rev {
		t.Fatal("heartbeat must not bump state_revision")
	}
	if int64Of(after, "playhead_sequence") != seq {
		t.Fatal("heartbeat must not bump playhead_sequence")
	}
}

func TestLeaveGuildUnbindStolenLeaseStillSucceeds(t *testing.T) {
	pool := discordTestPool(t)
	ctx := context.Background()
	play := playback.New(pool)
	guild := "w1b-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE kind='discord_guild' AND owner_key=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	a := New(pool, nil, nil, play, nil, nil)
	sid, err := a.ensureBoundSession(ctx, guild, "vc")
	if err != nil {
		t.Fatal(err)
	}
	b := New(pool, nil, nil, play, nil, nil)
	if _, err := b.ensureBoundSession(ctx, guild, "vc"); err != nil {
		t.Fatal(err)
	}
	if err := a.LeaveGuild(ctx, guild); err != nil {
		t.Fatalf("leave with stolen lease: %v", err)
	}
	st, err := play.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if holdsRendererLease(st, a.rendererID, a.generation) {
		t.Fatal("old worker must not still hold")
	}
	if !holdsRendererLease(st, b.rendererID, b.generation) {
		t.Fatal("winner lease must survive the stolen worker's leave")
	}
	var bound *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT session_id FROM discord_voice_runtime WHERE guild_id=$1`, guild).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if bound == nil || *bound != sid {
		t.Fatal("stolen leave must not clear the winner's runtime binding")
	}
}

func TestEnsureBoundSessionDoesNotStealBrowser(t *testing.T) {
	pool := discordTestPool(t)
	ctx := context.Background()
	play := playback.New(pool)
	userID := uuid.New()
	username := "w2-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_command_receipts WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_queue_items WHERE session_id IN (SELECT id FROM playback_sessions WHERE user_id=$1)`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	sid, err := play.WebSession(ctx, userID, "tab")
	if err != nil {
		t.Fatal(err)
	}
	guild := "w2-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, guild)
		_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, guild)
	})
	if _, err := play.BindGuildSession(ctx, guild, sid, "vc", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := play.AcquireBrowserRenderer(ctx, sid, "tab-1", 0); err != nil {
		t.Fatal(err)
	}

	bot := New(pool, nil, nil, play, nil, nil)
	got, err := bot.ensureBoundSession(ctx, guild, "vc")
	if err != nil {
		t.Fatal(err)
	}
	if got != sid {
		t.Fatalf("session %s want %s", got, sid)
	}
	st, err := play.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if rendererKindOf(st) != playback.RendererBrowser {
		t.Fatalf("worker stole browser, kind=%v", st["renderer_kind"])
	}
	if shouldEmitDiscordPCM(st, bot.rendererID, bot.generation) {
		t.Fatal("must not emit Discord PCM while browser holds")
	}
}

func TestPauseAfterReclaimDoesNotGlobalUpdate(t *testing.T) {
	pool := discordTestPool(t)
	ctx := context.Background()
	play := playback.New(pool)
	guild := "w1b-" + uuid.NewString()[:8]
	otherGuild := "w1b-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		for _, g := range []string{guild, otherGuild} {
			_, _ = pool.Exec(context.Background(), `DELETE FROM discord_voice_runtime WHERE guild_id=$1`, g)
			_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE kind='discord_guild' AND owner_key=$1`, g)
			_, _ = pool.Exec(context.Background(), `DELETE FROM discord_guilds WHERE id=$1`, g)
		}
	})
	old := New(pool, nil, nil, play, nil, nil)
	sid, err := old.ensureBoundSession(ctx, guild, "vc")
	if err != nil {
		t.Fatal(err)
	}
	if err := play.Control(ctx, sid, "resume", nil); err != nil {
		t.Fatal(err)
	}
	other, err := old.ensureBoundSession(ctx, otherGuild, "vc2")
	if err != nil {
		t.Fatal(err)
	}
	if err := play.Control(ctx, other, "resume", nil); err != nil {
		t.Fatal(err)
	}

	fresh := New(pool, nil, nil, play, nil, nil)
	fresh.resumeOnRestart = false
	if _, err := fresh.ensureBoundSession(ctx, guild, "vc"); err != nil {
		t.Fatal(err)
	}
	st, _ := play.Get(ctx, sid)
	if statusOf(st) == "playing" {
		t.Fatal("reclaimed playing session must not auto-resume")
	}
	otherSt, _ := play.Get(ctx, other)
	if statusOf(otherSt) != "playing" {
		t.Fatalf("must not globally pause other sessions, got %q", statusOf(otherSt))
	}
}
