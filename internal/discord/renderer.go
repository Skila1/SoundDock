package discordx

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sounddock/sounddock/internal/playback"
)

const rendererHeartbeatEvery = 15 * time.Second

type voiceRuntime struct {
	SessionID        uuid.UUID
	VoiceChannelID   string
	Connected        bool
	DisconnectReason string
	BindingRevision  int64
}

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func newRendererID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		id, err := uuid.NewRandom()
		if err != nil {
			return uuid.NewString()
		}
		return id.String()
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return uuid.UUID(raw).String()
}

func (b *Bot) resetRendererIdentity() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rendererID = newRendererID()
	b.generation = 1
}

func (b *Bot) rendererIdentity() (id string, gen int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rendererID == "" {
		b.rendererID = newRendererID()
	}
	if b.generation == 0 {
		b.generation = 1
	}
	return b.rendererID, b.generation
}

func (b *Bot) ensureRendererIdentity() {
	_, _ = b.rendererIdentity()
}

func (b *Bot) loadVoiceRuntime(ctx context.Context, guildID string) (voiceRuntime, error) {
	var rt voiceRuntime
	if b.pool == nil || guildID == "" {
		return rt, nil
	}
	var sid *uuid.UUID
	var ch, reason *string
	err := b.pool.QueryRow(ctx, `
		SELECT session_id, voice_channel_id, connected, last_disconnect_reason, binding_revision
		FROM discord_voice_runtime WHERE guild_id=$1`, guildID).
		Scan(&sid, &ch, &rt.Connected, &reason, &rt.BindingRevision)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return rt, nil
		}
		return rt, err
	}
	if sid != nil {
		rt.SessionID = *sid
	}
	if ch != nil {
		rt.VoiceChannelID = *ch
	}
	if reason != nil {
		rt.DisconnectReason = *reason
	}
	return rt, nil
}

func (b *Bot) voiceChannelForGuild(guildID string) string {
	if ch, ok := b.BotChannel(guildID); ok && ch != "" {
		return ch
	}
	if b.pool == nil {
		return ""
	}
	var ch *string
	_ = b.pool.QueryRow(context.Background(), `
		SELECT voice_channel_id FROM discord_voice_runtime WHERE guild_id=$1`, guildID).Scan(&ch)
	if ch != nil {
		return *ch
	}
	return ""
}

const staleBrowserLease = 45 * time.Second

// shouldClaimDiscordLease is true when this worker may CAS-claim Discord.
// A live Browser tab still wins. An empty holder (closed or refreshed tab)
// must be claimable even if leftover output_pref is browser.
func shouldClaimDiscordLease(kind, outputPref, rendererKind string) bool {
	_ = kind
	_ = outputPref
	return rendererKind != playback.RendererBrowser
}

// shouldEmitDiscordPCM is true only when this worker holds the Discord lease
// and the session wants Discord output. Browser output stays in VC silently.
func shouldEmitDiscordPCM(st map[string]any, rendererID string, generation int64) bool {
	if !holdsRendererLease(st, rendererID, generation) {
		return false
	}
	return outputPrefOf(st) == playback.OutputDiscord
}

func outputPrefOf(st map[string]any) string {
	s, _ := st["output_pref"].(string)
	return s
}

func kindOf(st map[string]any) string {
	s, _ := st["kind"].(string)
	return s
}

// ensureBoundSession binds the guild to a playback session without granting a
// lease. It claims Discord only when shouldClaimDiscordLease is true (never
// steals Browser). HTTP join is bind-only; slash /play claims guild-native.
func (b *Bot) ensureBoundSession(ctx context.Context, guildID, channelID string) (uuid.UUID, error) {
	if b.play == nil {
		return uuid.Nil, errors.New("playback engine is not configured")
	}
	rid, gen := b.rendererIdentity()
	rt, err := b.loadVoiceRuntime(ctx, guildID)
	if err != nil {
		return uuid.Nil, err
	}
	ch := channelID
	if ch == "" {
		ch = rt.VoiceChannelID
	}
	if ch == "" {
		ch = b.voiceChannelForGuild(guildID)
	}

	sid := rt.SessionID
	var prev map[string]any
	if sid != uuid.Nil {
		st, err := b.play.Get(ctx, sid)
		if err == nil {
			prev = st
			if rendererKindOf(st) == playback.RendererBrowser {
				if released, _ := b.play.ReleaseStaleBrowserRenderer(ctx, sid, staleBrowserLease); released {
					if fresh, ferr := b.play.Get(ctx, sid); ferr == nil {
						st = fresh
						prev = fresh
					}
				}
			}
			sameCh := ch == "" || rt.VoiceChannelID == ch
			if sameCh && holdsRendererLease(st, rid, gen) {
				b.lastBindRev.Store(guildID, rt.BindingRevision)
				return sid, nil
			}
			if sameCh && !shouldClaimDiscordLease(kindOf(st), outputPrefOf(st), rendererKindOf(st)) {
				b.lastBindRev.Store(guildID, rt.BindingRevision)
				return sid, nil
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, err
		} else {
			sid = uuid.Nil
		}
	}
	if sid == uuid.Nil {
		sid, err = b.play.Session(ctx, "discord_guild", guildID, nil)
		if err != nil {
			return uuid.Nil, err
		}
	}

	br, err := b.play.BindGuildSession(ctx, guildID, sid, ch, rt.BindingRevision)
	if err != nil {
		return uuid.Nil, err
	}
	b.lastBindRev.Store(guildID, br.BindingRevision)

	st, err := b.play.Get(ctx, sid)
	if err != nil {
		return uuid.Nil, err
	}
	if holdsRendererLease(st, rid, gen) {
		return sid, nil
	}
	if !shouldClaimDiscordLease(kindOf(st), outputPrefOf(st), rendererKindOf(st)) {
		return sid, nil
	}
	if err := b.play.ClaimDiscordRenderer(ctx, sid, rid, gen, false); err != nil {
		if errors.Is(err, playback.ErrLeaseConflict) {
			return sid, nil
		}
		return uuid.Nil, err
	}
	if prev != nil {
		b.pauseIfStaleDiscordReclaim(ctx, sid, prev)
	}
	return sid, nil
}

func (b *Bot) boundState(ctx context.Context, guildID, channelID string) (uuid.UUID, map[string]any, error) {
	sid, err := b.ensureBoundSession(ctx, guildID, channelID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	st, err := b.play.Get(ctx, sid)
	if err != nil {
		return uuid.Nil, nil, err
	}
	return sid, st, nil
}

func (b *Bot) pauseIfStaleDiscordReclaim(ctx context.Context, sid uuid.UUID, st map[string]any) {
	if b.resumeOnRestart || st == nil {
		return
	}
	rid, gen := b.rendererIdentity()
	if !shouldPauseAfterReclaim(false, rendererKindOf(st), rendererIDOf(st), int64Of(st, "renderer_generation"), rid, gen, statusOf(st)) {
		return
	}
	_ = b.play.Control(ctx, sid, "pause", nil)
}

func shouldPauseAfterReclaim(resumeOnRestart bool, prevKind, prevID string, prevGen int64, newID string, newGen int64, status string) bool {
	if resumeOnRestart || status != "playing" {
		return false
	}
	return prevKind == playback.RendererDiscord && (prevID != newID || prevGen != newGen)
}

func holdsRendererLease(st map[string]any, rendererID string, generation int64) bool {
	if st == nil || rendererID == "" {
		return false
	}
	if rendererKindOf(st) != playback.RendererDiscord {
		return false
	}
	return rendererIDOf(st) == rendererID && int64Of(st, "renderer_generation") == generation
}

func (b *Bot) heartbeatLease(ctx context.Context, sid uuid.UUID) error {
	if b.play == nil || sid == uuid.Nil {
		return nil
	}
	rid, gen := b.rendererIdentity()
	return b.play.HeartbeatRenderer(ctx, sid, playback.RendererDiscord, rid, gen)
}

func (b *Bot) heartbeatHeldLeases(ctx context.Context) {
	if b.pool == nil || b.play == nil {
		return
	}
	rows, err := b.pool.Query(ctx, `SELECT session_id FROM discord_voice_runtime WHERE session_id IS NOT NULL`)
	if err != nil {
		return
	}
	defer rows.Close()
	rid, gen := b.rendererIdentity()
	for rows.Next() {
		var sid uuid.UUID
		if rows.Scan(&sid) != nil || sid == uuid.Nil {
			continue
		}
		if err := b.play.HeartbeatRenderer(ctx, sid, playback.RendererDiscord, rid, gen); err != nil && !errors.Is(err, playback.ErrLeaseConflict) && b.log != nil {
			b.log.Debug("discord renderer heartbeat", "err", err)
		}
	}
}

func (b *Bot) claimDiscordForCommand(ctx context.Context, sid uuid.UUID) error {
	if b == nil || b.play == nil || sid == uuid.Nil {
		return nil
	}
	rid, gen := b.rendererIdentity()
	return b.play.ClaimDiscordRenderer(ctx, sid, rid, gen, true)
}

func extraWithCommandID(extra map[string]any, commandID string) map[string]any {
	out := map[string]any{}
	for k, v := range extra {
		out[k] = v
	}
	if commandID != "" {
		out["command_id"] = commandID
	}
	return out
}

func mutatingSlashCommand(name string) bool {
	switch name {
	case "pause", "resume", "skip", "previous", "stop", "clear", "shuffle", "repeat", "volume":
		return true
	default:
		return false
	}
}

func rendererKindOf(st map[string]any) string {
	s, _ := st["renderer_kind"].(string)
	return s
}

func statusOf(st map[string]any) string {
	s, _ := st["status"].(string)
	return s
}

func rendererIDOf(st map[string]any) string {
	switch v := st["renderer_id"].(type) {
	case string:
		return v
	case *string:
		if v != nil {
			return *v
		}
	}
	return ""
}

func int64Of(st map[string]any, key string) int64 {
	switch v := st[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case float64:
		return int64(v)
	case *int64:
		if v != nil {
			return *v
		}
	}
	return 0
}

func playbackInstanceID(st map[string]any) uuid.UUID {
	if st == nil {
		return uuid.Nil
	}
	switch v := st["playback_instance_id"].(type) {
	case uuid.UUID:
		return v
	case *uuid.UUID:
		if v != nil {
			return *v
		}
	case string:
		id, err := uuid.Parse(v)
		if err == nil {
			return id
		}
	}
	return uuid.Nil
}

func initResumeOnRestart() bool {
	return envTruthy(os.Getenv("SD_DISCORD_RESUME_ON_RESTART"))
}
