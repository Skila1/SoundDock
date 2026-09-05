package discordx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/mediabusy"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/scrobble"
	"github.com/sounddock/sounddock/internal/storage"
)

type guildRuntime struct {
	cancel context.CancelFunc
	gain   *pcmGain
}

var idleSince sync.Map

func sendOpus(ctx context.Context, vc *discordgo.VoiceConnection, pkt []byte) (ok bool) {
	if vc == nil || vc.OpusSend == nil || vc.Status == discordgo.VoiceConnectionStatusDead {
		return false
	}
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	select {
	case <-ctx.Done():
		return false
	case vc.OpusSend <- pkt:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

func disconnectVC(vc *discordgo.VoiceConnection) {
	if vc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = vc.Disconnect(ctx)
}

func (b *Bot) dropVoice(guildID string) {
	sess := b.session()
	if sess == nil {
		return
	}
	if vc, ok := sess.VoiceConnections[guildID]; ok && vc != nil {
		disconnectVC(vc)
	}
	delete(sess.VoiceConnections, guildID)
}

func waitVoiceReady(vc *discordgo.VoiceConnection, d time.Duration) bool {
	if vc == nil {
		return false
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if vc.Status == discordgo.VoiceConnectionStatusDead {
			return false
		}
		if vc.Status == discordgo.VoiceConnectionStatusReady && vc.OpusSend != nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return vc.Status == discordgo.VoiceConnectionStatusReady && vc.OpusSend != nil
}

func waitDAVEReady(vc *discordgo.VoiceConnection, d time.Duration) error {
	if vc == nil {
		return fmt.Errorf("no voice connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	if err := vc.WaitForDAVEReady(ctx); err != nil {
		return err
	}
	return nil
}

func (b *Bot) voiceConn(guildID string) *discordgo.VoiceConnection {
	sess := b.session()
	if sess == nil {
		return nil
	}
	vc, ok := sess.VoiceConnections[guildID]
	if !ok || vc == nil || vc.Status == discordgo.VoiceConnectionStatusDead {
		return nil
	}
	return vc
}

// BotChannel is the voice channel the bot is actually in for this guild.
func (b *Bot) BotChannel(guildID string) (string, bool) {
	sess := b.session()
	if sess == nil || sess.State == nil || sess.State.User == nil {
		return "", false
	}
	st, err := sess.State.VoiceState(guildID, sess.State.User.ID)
	if err != nil || st == nil || st.ChannelID == "" {
		return "", false
	}
	return st.ChannelID, true
}

// MarkVoiceDisconnected clears a leftover runtime row so web clients stop
// attaching to a voice session the bot is not actually in.
func MarkVoiceDisconnected(ctx context.Context, pool *pgxpool.Pool, guildID, reason string) {
	if pool == nil || guildID == "" {
		return
	}
	if reason == "" {
		reason = "disconnected"
	}
	_, _ = pool.Exec(ctx, `
		UPDATE discord_voice_runtime
		SET connected=false, voice_channel_id=NULL, last_disconnect_reason=$2
		WHERE guild_id=$1`, guildID, reason)
}

func (b *Bot) markVoiceDisconnected(ctx context.Context, guildID, reason string) {
	if b == nil {
		return
	}
	MarkVoiceDisconnected(ctx, b.pool, guildID, reason)
}

func (b *Bot) markVoiceConnected(ctx context.Context, guildID, channelID string, sid uuid.UUID) {
	_ = sid
	_, _ = b.pool.Exec(ctx, `
		UPDATE discord_voice_runtime
		SET voice_channel_id=$2, connected=true, last_disconnect_reason=''
		WHERE guild_id=$1`, guildID, channelID)
	b.ensureStreamer(guildID)
}

func (b *Bot) markJoining(ctx context.Context, guildID, channelID string, sid uuid.UUID) {
	_ = sid
	_, _ = b.pool.Exec(ctx, `
		UPDATE discord_voice_runtime
		SET voice_channel_id=$2, connected=false, last_disconnect_reason='joining'
		WHERE guild_id=$1`, guildID, channelID)
}

func (b *Bot) finishVoiceJoin(ctx context.Context, vc *discordgo.VoiceConnection, guildID, channelID string, sid uuid.UUID) error {
	if !waitVoiceReady(vc, 15*time.Second) {
		b.dropVoice(guildID)
		b.markVoiceDisconnected(ctx, guildID, "timeout waiting for voice")
		return fmt.Errorf("timeout waiting for voice")
	}
	if err := waitDAVEReady(vc, 15*time.Second); err != nil {
		b.dropVoice(guildID)
		b.markVoiceDisconnected(ctx, guildID, "timeout waiting for DAVE")
		return fmt.Errorf("timeout waiting for DAVE: %w", err)
	}
	b.markVoiceConnected(ctx, guildID, channelID, sid)
	return nil
}

// JoinChannel connects the bot to a guild voice channel and starts PCM streaming
// from the discord_guild playback session. Reuses an existing healthy connection.
func (b *Bot) JoinChannel(ctx context.Context, guildID, channelID string) error {
	if guildID == "" || channelID == "" {
		return fmt.Errorf("not in a voice channel")
	}
	sess := b.session()
	if sess == nil {
		return fmt.Errorf("discord gateway is not connected")
	}
	_, _ = b.pool.Exec(ctx, `INSERT INTO discord_guilds (id) VALUES ($1) ON CONFLICT DO NOTHING`, guildID)
	sid, err := b.ensureBoundSession(ctx, guildID, channelID)
	if err != nil {
		return err
	}

	if vc := b.voiceConn(guildID); vc != nil {
		if cur, ok := b.BotChannel(guildID); ok && cur == channelID && waitVoiceReady(vc, 0) {
			if err := waitDAVEReady(vc, 5*time.Second); err == nil {
				b.markVoiceConnected(ctx, guildID, channelID, sid)
				return nil
			}
		}
		if cur, ok := b.BotChannel(guildID); ok && cur != channelID {
			if err := sess.VoiceStateUpdate(guildID, channelID, false, true); err == nil {
				if err := b.finishVoiceJoin(ctx, vc, guildID, channelID, sid); err == nil {
					return nil
				}
			}
		}
	}

	b.markJoining(ctx, guildID, channelID, sid)
	b.dropVoice(guildID)
	jctx, jcancel := context.WithTimeout(ctx, 20*time.Second)
	defer jcancel()
	// Deafened like a playback-only bot: we send audio, we do not need to receive it.
	vc, err := sess.ChannelVoiceJoin(jctx, guildID, channelID, false, true)
	if err != nil {
		b.dropVoice(guildID)
		b.markVoiceDisconnected(ctx, guildID, redacted(err.Error()))
		return err
	}
	return b.finishVoiceJoin(ctx, vc, guildID, channelID, sid)
}

func (b *Bot) stopStreamer(guildID string) {
	if v, ok := b.voices.LoadAndDelete(guildID); ok {
		if gr, ok := v.(*guildRuntime); ok && gr.cancel != nil {
			gr.cancel()
		}
	}
}

// LeaveGuild disconnects voice, stops the PCM streamer, and CAS-unbinds this
// worker's Discord renderer. connected=false is written first while we still
// hold the lease so reconcile cannot restart the streamer. If the lease was
// already stolen, voice is still dropped and Unbind is a no-op (bind_conflict).
func (b *Bot) LeaveGuild(ctx context.Context, guildID string) error {
	rt, _ := b.loadVoiceRuntime(ctx, guildID)
	rid, gen := b.rendererIdentity()
	held := false
	if rt.SessionID != uuid.Nil && b.play != nil {
		if st, err := b.play.Get(ctx, rt.SessionID); err == nil && holdsRendererLease(st, rid, gen) {
			held = true
		}
	}
	b.markVoiceDisconnected(ctx, guildID, "leave")
	b.stopStreamer(guildID)
	b.dropVoice(guildID)
	if held && rt.SessionID != uuid.Nil && b.play != nil {
		_ = b.play.Control(ctx, rt.SessionID, "stop", nil)
		_ = b.play.Control(ctx, rt.SessionID, "clear", map[string]any{"all": true})
	}
	if held && b.play != nil {
		expected := rt.BindingRevision
		if v, ok := b.lastBindRev.Load(guildID); ok {
			if n, ok := v.(int64); ok {
				expected = n
			}
		}
		_, err := b.play.UnbindDiscordRenderer(ctx, guildID, expected, rid, gen)
		if err != nil && !errors.Is(err, playback.ErrBindConflict) && !errors.Is(err, playback.ErrLeaseConflict) && b.log != nil {
			b.log.Warn("discord unbind", "guild", guildID, "err", err)
		}
	}
	b.lastBindRev.Delete(guildID)
	return nil
}

func (b *Bot) ensureStreamer(guildID string) {
	if _, ok := b.voices.Load(guildID); ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	gr := &guildRuntime{cancel: cancel, gain: newPCMGain(1)}
	if _, loaded := b.voices.LoadOrStore(guildID, gr); loaded {
		cancel()
		return
	}
	go b.streamLoop(ctx, guildID)
}

func (b *Bot) streamGain(guildID string) *pcmGain {
	v, ok := b.voices.Load(guildID)
	if !ok {
		return nil
	}
	gr, ok := v.(*guildRuntime)
	if !ok {
		return nil
	}
	return gr.gain
}

func (b *Bot) streamLoop(ctx context.Context, guildID string) {
	defer b.voices.Delete(guildID)
	var (
		trackCancel  context.CancelFunc
		current      uuid.UUID
		status       string
		appliedStart int
		appliedAt    time.Time
	)
	stopTrack := func() {
		if trackCancel != nil {
			trackCancel()
			trackCancel = nil
		}
	}
	defer stopTrack()
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	var lastHB time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		rt, err := b.loadVoiceRuntime(ctx, guildID)
		if err != nil {
			continue
		}
		if !rt.Connected {
			if rt.DisconnectReason == "joining" {
				stopTrack()
				continue
			}
			// LeaveGuild / kick set connected=false; do not keep playing just
			// because the voice socket has not finished closing.
			stopTrack()
			return
		}
		sid, st, err := b.boundState(ctx, guildID, rt.VoiceChannelID)
		if err != nil {
			stopTrack()
			if errors.Is(err, playback.ErrBindConflict) || errors.Is(err, playback.ErrLeaseConflict) {
				return
			}
			continue
		}
		rid, gen := b.rendererIdentity()
		if !shouldEmitDiscordPCM(st, rid, gen) {
			// Browser (or other) output: stay in VC, do not unbind, do not play.
			stopTrack()
			continue
		}
		if time.Since(lastHB) >= rendererHeartbeatEvery {
			if err := b.heartbeatLease(ctx, sid); errors.Is(err, playback.ErrLeaseConflict) {
				stopTrack()
				return
			}
			lastHB = time.Now()
		}
		stat, _ := st["status"].(string)
		var tid uuid.UUID
		switch v := st["current_track_id"].(type) {
		case uuid.UUID:
			tid = v
		case *uuid.UUID:
			if v != nil {
				tid = *v
			}
		}
		if stat != "playing" || tid == uuid.Nil {
			if stat != status {
				stopTrack()
				status = stat
			}
			continue
		}
		// Volume/mute are live PCM gain: update the multiplier in place. Do not
		// stopTrack or reconnect voice - state_revision is the engine's job.
		if g := b.streamGain(guildID); g != nil {
			g.Set(liveVolumeMultiplier(st))
		}
		if tid == current && stat == status && trackCancel != nil {
			pos := sessionPositionMS(st)
			expected := appliedStart + int(time.Since(appliedAt).Milliseconds())
			if pos-expected > 2000 || expected-pos > 2000 {
				stopTrack()
			}
			continue
		}
		stopTrack()
		current, status = tid, stat
		appliedStart = sessionPositionMS(st)
		appliedAt = time.Now()
		tctx, cancel := context.WithCancel(ctx)
		trackCancel = cancel
		go b.playTrack(tctx, guildID, sid, tid, st)
	}
}

func (b *Bot) playTrack(ctx context.Context, guildID string, sid, trackID uuid.UUID, st map[string]any) {
	sess := b.session()
	if sess == nil {
		b.recordPlaybackError(ctx, guildID, trackID, "voice", "discord session missing")
		return
	}
	vc, ok := sess.VoiceConnections[guildID]
	if !ok || vc == nil {
		b.recordPlaybackError(ctx, guildID, trackID, "voice", "not connected to voice")
		return
	}
	if !waitVoiceReady(vc, 15*time.Second) {
		b.recordPlaybackError(ctx, guildID, trackID, "voice", "voice connection not ready")
		return
	}
	if err := waitDAVEReady(vc, 15*time.Second); err != nil {
		b.recordPlaybackError(ctx, guildID, trackID, "voice", "DAVE not ready")
		return
	}

	src, gainDB, durationMS, err := b.ffmpegSourceForTrack(ctx, trackID, st)
	if err != nil {
		b.recordPlaybackError(ctx, guildID, trackID, "ffmpeg", err.Error())
		_ = b.play.Control(ctx, sid, "skip", skipControlExtra(false))
		return
	}
	defer src.Close()

	startMS := sessionPositionMS(st)
	pcmCmd, pcm, err := src.open(ctx, gainDB, startMS)
	if err != nil {
		b.recordPlaybackError(ctx, guildID, trackID, "ffmpeg", err.Error())
		_ = b.play.Control(ctx, sid, "skip", skipControlExtra(false))
		return
	}
	holder := "discord:" + sid.String()
	switch g := st["renderer_generation"].(type) {
	case int64:
		if g > 0 {
			holder = holder + ":" + strconv.FormatInt(g, 10)
		}
	case float64:
		if g > 0 {
			holder = holder + ":" + strconv.FormatInt(int64(g), 10)
		}
	}
	releaseBusy := b.MediaBusy.Acquire(ctx, trackID, mediabusy.KindDiscord, holder)
	defer releaseBusy()
	b.MediaBusy.UpdateTrack(ctx, mediabusy.KindDiscord, holder, trackID)
	defer func() {
		if pcm != nil {
			pcm.Close()
		}
		if pcmCmd != nil && pcmCmd.Process != nil {
			_ = pcmCmd.Process.Kill()
			_ = pcmCmd.Wait()
		}
	}()

	gain := b.streamGain(guildID)
	if gain == nil {
		gain = newPCMGain(liveVolumeMultiplier(st))
	} else {
		gain.Set(liveVolumeMultiplier(st))
	}
	enc, err := startOpusEncoder(ctx, newPCMGainReader(pcm, gain))
	if err != nil {
		b.recordPlaybackError(ctx, guildID, trackID, "opus", err.Error())
		return
	}
	defer enc.Close()

	_ = vc.Speaking(true)
	defer vc.Speaking(false)

	started := time.Now()
	lastPosWrite := time.Time{}
	instanceID := playbackInstanceID(st)
	thresh := durationMS / 2
	if thresh > 30000 {
		thresh = 30000
	}
	counted := durationMS > 0 && startMS >= thresh
	for {
		pkt, err := enc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.recordPlaybackError(ctx, guildID, trackID, "opus", err.Error())
			break
		}
		if !sendOpus(ctx, vc, pkt) {
			if ctx.Err() == nil {
				b.recordPlaybackError(ctx, guildID, trackID, "voice", "opus send failed")
			}
			return
		}
		elapsed := startMS + int(time.Since(started).Milliseconds())
		if ctx.Err() != nil {
			return
		}
		if time.Since(lastPosWrite) >= time.Second {
			lastPosWrite = time.Now()
			if b.MediaBusy != nil {
				b.MediaBusy.Heartbeat(ctx, mediabusy.KindDiscord, holder)
			}
			if instanceID == uuid.Nil {
				if cur, err := b.play.Get(ctx, sid); err == nil {
					instanceID = playbackInstanceID(cur)
				}
			}
			if instanceID == uuid.Nil {
				b.recordPlaybackError(ctx, guildID, trackID, "playhead", "playback instance missing")
				return
			}
			if err := b.play.CheckpointPlayhead(ctx, sid, instanceID, elapsed); err != nil {
				return
			}
			b.recordDiscordListens(ctx, guildID, sid, trackID, durationMS, elapsed, instanceID)
		}
		if !counted && durationMS > 0 && elapsed >= thresh {
			counted = true
			b.recordDiscordListens(ctx, guildID, sid, trackID, durationMS, elapsed, instanceID)
		}
	}
	if ctx.Err() != nil {
		return
	}
	// Natural end: advance the guild session. Repeat-one loops only when ended=true.
	_ = b.play.Control(context.Background(), sid, "skip", skipControlExtra(true))
}

func skipControlExtra(ended bool) map[string]any {
	if ended {
		return map[string]any{"ended": true}
	}
	return nil
}

type pcmSource struct {
	path   string
	reader io.ReadCloser
	close  func() error
}

func (s pcmSource) Close() {
	if s.close != nil {
		_ = s.close()
	}
}

func (s pcmSource) open(ctx context.Context, gainDB float64, startMS int) (*exec.Cmd, io.ReadCloser, error) {
	if s.path != "" {
		return FFmpegPCM(ctx, s.path, gainDB, startMS)
	}
	if s.reader != nil {
		return ffmpegPCMReader(ctx, s.reader, gainDB, startMS)
	}
	return nil, nil, fmt.Errorf("no ffmpeg source")
}

func sessionPositionMS(st map[string]any) int {
	if st == nil {
		return 0
	}
	switch v := st["position_ms"].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

// sessionVolume returns the session volume in 0-1. Missing volume defaults to 1;
// an explicit 0 is silence and must not be rewritten to 100%.
func sessionVolume(st map[string]any) float64 {
	if st == nil {
		return 1
	}
	v, ok := st["volume"]
	if !ok || v == nil {
		return 1
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 1
	}
}

func sessionMuted(st map[string]any) bool {
	if st == nil {
		return false
	}
	v, ok := st["muted"].(bool)
	return ok && v
}

// liveVolumeMultiplier is the extra PCM scale applied after ReplayGain.
// Session volume is 0-1 (engine clamps >1); mute or volume 0 is silence.
func liveVolumeMultiplier(st map[string]any) float64 {
	if sessionMuted(st) {
		return 0
	}
	v := sessionVolume(st)
	if v <= 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// pcmGainDB is ReplayGain only, baked into the first ffmpeg -af at track start.
// Live session volume/mute is a separate int16 multiplier on the s16le pipe.
func pcmGainDB(mode string, trackGain, albumGain *float64) float64 {
	mult := playback.ReplayGainMultiplier(mode, trackGain, albumGain, 0)
	if mult > 0 && mult != 1 {
		return 20 * math.Log10(mult)
	}
	return 0
}

func (b *Bot) ffmpegSourceForTrack(ctx context.Context, trackID uuid.UUID, st map[string]any) (pcmSource, float64, int, error) {
	var libID uuid.UUID
	var key string
	var trackGain, albumGain *float64
	var duration int
	err := b.pool.QueryRow(ctx, `
		SELECT tf.library_id, tf.storage_key, tf.replaygain_track_gain, tf.replaygain_album_gain, t.duration_ms
		FROM track_files tf JOIN tracks t ON t.id=tf.track_id
		WHERE tf.track_id=$1
		ORDER BY CASE tf.quality WHEN 'original' THEN 0 ELSE 1 END
		LIMIT 1`, trackID).Scan(&libID, &key, &trackGain, &albumGain, &duration)
	if err != nil {
		return pcmSource{}, 0, 0, err
	}
	if b.provider == nil {
		return pcmSource{}, 0, 0, fmt.Errorf("no storage provider")
	}
	prov, _, _, err := b.provider(ctx, libID)
	if err != nil {
		return pcmSource{}, 0, 0, err
	}
	mode, _ := st["replaygain_mode"].(string)
	gainDB := pcmGainDB(mode, trackGain, albumGain)
	src := pcmSource{}
	if fs, ok := prov.(storage.FFmpegSourcer); ok {
		ff, err := fs.FFmpegSource(ctx, key)
		if err != nil {
			return pcmSource{}, 0, 0, err
		}
		src.path = ff.Path
		src.reader = ff.Reader
		src.close = ff.Close
		return src, gainDB, duration, nil
	}
	rc, _, err := prov.Open(ctx, key)
	if err != nil {
		return pcmSource{}, 0, 0, err
	}
	src.reader = rc
	src.close = rc.Close
	return src, gainDB, duration, nil
}

func (b *Bot) recordPlaybackError(ctx context.Context, guildID string, trackID uuid.UUID, class, msg string) {
	_, _ = b.pool.Exec(ctx, `INSERT INTO discord_playback_errors (guild_id, track_id, error_class, message) VALUES ($1,$2,$3,$4)`,
		guildID, trackID, class, redacted(msg))
}

func (b *Bot) recordDiscordListens(ctx context.Context, guildID string, sid, trackID uuid.UUID, durationMS, positionMS int, instanceID uuid.UUID) {
	if b.scrobble == nil {
		return
	}
	rendererKind := playback.RendererDiscord
	seq := int64(0)
	rate := 1.0
	status := "playing"
	if b.play != nil && sid != uuid.Nil {
		st, err := b.play.Get(ctx, sid)
		if err != nil {
			return
		}
		rendererKind = rendererKindOf(st)
		if rendererKind != playback.RendererDiscord {
			return
		}
		if instanceID == uuid.Nil {
			instanceID = playbackInstanceID(st)
		}
		seq = int64Of(st, "playhead_sequence")
		status = statusOf(st)
		switch v := st["playback_rate"].(type) {
		case float64:
			if v > 0 {
				rate = v
			}
		case float32:
			if v > 0 {
				rate = float64(v)
			}
		}
	}
	rows, err := b.pool.Query(ctx, `
		SELECT i.user_id FROM discord_user_voice v
		JOIN user_identities i ON i.provider='discord' AND i.provider_user_id=v.discord_user_id
		JOIN discord_voice_runtime r ON r.guild_id=v.guild_id
		WHERE v.guild_id=$1 AND v.channel_id=r.voice_channel_id
			AND r.voice_channel_id IS NOT NULL AND r.voice_channel_id <> ''`, guildID)
	if err != nil {
		return
	}
	defer rows.Close()
	audio := true
	for rows.Next() {
		var uid uuid.UUID
		if rows.Scan(&uid) != nil {
			continue
		}
		_ = b.scrobble.HandleListen(ctx, uid, scrobble.Event{
			TrackID: trackID, PositionMS: positionMS, DurationMS: durationMS, Source: "discord", Kind: "progress",
			PlaybackInstanceID: instanceID, PlayheadSequence: seq,
			RendererKind: rendererKind, PlaybackRate: rate, Status: status,
			AudioListener: &audio,
		})
	}
}

func (b *Bot) reconcileVoice(ctx context.Context) {
	rows, err := b.pool.Query(ctx, `SELECT guild_id, voice_channel_id, connected, last_disconnect_reason FROM discord_voice_runtime`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var gid string
		var ch *string
		var connected bool
		var reason *string
		if rows.Scan(&gid, &ch, &connected, &reason) != nil {
			continue
		}
		wantJoin := !connected && ch != nil && *ch != "" && reason != nil && *reason == "pending_join"
		if wantJoin {
			if err := b.JoinChannel(ctx, gid, *ch); err != nil {
				b.log.Warn("pending join", "guild", gid, "err", err)
			}
			continue
		}
		if connected {
			live, inVC := b.BotChannel(gid)
			if !inVC || ch == nil || *ch == "" || live != *ch {
				b.markVoiceDisconnected(ctx, gid, "stale_runtime")
				b.stopStreamer(gid)
				continue
			}
		}
		if !connected {
			if reason != nil && *reason == "joining" {
				continue
			}
			b.stopStreamer(gid)
		} else if ch != nil && *ch != "" && b.voiceConn(gid) != nil {
			if _, err := b.ensureBoundSession(ctx, gid, *ch); err != nil {
				if errors.Is(err, playback.ErrBindConflict) || errors.Is(err, playback.ErrLeaseConflict) {
					b.stopStreamer(gid)
					continue
				}
			}
			b.ensureStreamer(gid)
		}
	}
	b.maybeIdleLeave(ctx)
}

func (b *Bot) maybeIdleLeave(ctx context.Context) {
	sess := b.session()
	if sess == nil || sess.State == nil {
		return
	}
	rows, err := b.pool.Query(ctx, `SELECT id, inactivity_leave_empty_minutes, inactivity_leave_no_listeners_minutes, stay_while_queue_nonempty FROM discord_guilds WHERE enabled=true`)
	if err != nil {
		return
	}
	defer rows.Close()
	botID := ""
	if sess.State.User != nil {
		botID = sess.State.User.ID
	}
	for rows.Next() {
		var gid string
		var emptyMin, noListenMin int
		var stay bool
		if rows.Scan(&gid, &emptyMin, &noListenMin, &stay) != nil {
			continue
		}
		var connected bool
		var ch *string
		_ = b.pool.QueryRow(ctx, `SELECT connected, voice_channel_id FROM discord_voice_runtime WHERE guild_id=$1`, gid).Scan(&connected, &ch)
		if !connected || ch == nil || *ch == "" {
			idleSince.Delete(gid)
			continue
		}
		humans := 0
		g, err := sess.State.Guild(gid)
		if err == nil && g != nil {
			for _, vs := range g.VoiceStates {
				if vs.ChannelID == *ch && vs.UserID != botID {
					humans++
				}
			}
		}
		if humans > 0 {
			idleSince.Delete(gid)
			continue
		}
		if stay {
			rt, err := b.loadVoiceRuntime(ctx, gid)
			if err == nil && rt.SessionID != uuid.Nil {
				items, _ := b.play.Queue(ctx, rt.SessionID)
				st, _ := b.play.Get(ctx, rt.SessionID)
				stat, _ := st["status"].(string)
				if len(items) > 0 && stat == "playing" {
					idleSince.Delete(gid)
					continue
				}
			}
		}
		mins := emptyMin
		if noListenMin > 0 && (mins == 0 || noListenMin < mins) {
			mins = noListenMin
		}
		if mins <= 0 {
			mins = 5
		}
		now := time.Now()
		v, ok := idleSince.Load(gid)
		if !ok {
			idleSince.Store(gid, now)
			continue
		}
		since, _ := v.(time.Time)
		if now.Sub(since) < time.Duration(mins)*time.Minute {
			continue
		}
		idleSince.Delete(gid)
		_ = b.LeaveGuild(ctx, gid)
	}
}
