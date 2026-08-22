package discordx

import (
	"context"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/scrobble"
	"github.com/sounddock/sounddock/internal/storage"
)

type guildRuntime struct {
	cancel context.CancelFunc
}

var idleSince sync.Map

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

// JoinChannel connects the bot to a guild voice channel and starts PCM streaming
// from the discord_guild playback session. Reuses discord_voice_runtime.
func (b *Bot) JoinChannel(ctx context.Context, guildID, channelID string) error {
	if guildID == "" || channelID == "" {
		return fmt.Errorf("not in a voice channel")
	}
	sess := b.session()
	if sess == nil {
		return fmt.Errorf("discord gateway is not connected")
	}
	_, _ = b.pool.Exec(ctx, `INSERT INTO discord_guilds (id) VALUES ($1) ON CONFLICT DO NOTHING`, guildID)
	sid, err := b.play.Session(ctx, "discord_guild", guildID, nil)
	if err != nil {
		return err
	}
	_, _ = b.pool.Exec(ctx, `
		INSERT INTO discord_voice_runtime (guild_id, voice_channel_id, session_id, connected, last_disconnect_reason)
		VALUES ($1,$2,$3,false,'joining')
		ON CONFLICT (guild_id) DO UPDATE SET voice_channel_id=$2, session_id=$3, connected=false, last_disconnect_reason='joining'`,
		guildID, channelID, sid)

	if v, ok := b.voices.LoadAndDelete(guildID); ok {
		if gr, ok := v.(*guildRuntime); ok && gr.cancel != nil {
			gr.cancel()
		}
	}
	// A timed-out VoiceConnection keeps retrying and looks like join/leave.
	b.dropVoice(guildID)
	jctx, jcancel := context.WithTimeout(ctx, 20*time.Second)
	defer jcancel()
	vc, err := sess.ChannelVoiceJoin(jctx, guildID, channelID, false, false)
	if err != nil {
		b.dropVoice(guildID)
		_, _ = b.pool.Exec(ctx, `UPDATE discord_voice_runtime SET connected=false, last_disconnect_reason=$2 WHERE guild_id=$1`, guildID, redacted(err.Error()))
		return err
	}
	if !waitVoiceReady(vc, 5*time.Second) {
		b.dropVoice(guildID)
		_, _ = b.pool.Exec(ctx, `UPDATE discord_voice_runtime SET connected=false, last_disconnect_reason=$2 WHERE guild_id=$1`, guildID, "timeout waiting for voice")
		return fmt.Errorf("timeout waiting for voice")
	}
	// Discord rejects non-DAVE voice with close 4017. Do not mark connected until E2EE is up.
	if err := waitDAVEReady(vc, 15*time.Second); err != nil {
		b.dropVoice(guildID)
		_, _ = b.pool.Exec(ctx, `UPDATE discord_voice_runtime SET connected=false, last_disconnect_reason=$2 WHERE guild_id=$1`, guildID, "timeout waiting for DAVE")
		return fmt.Errorf("timeout waiting for DAVE: %w", err)
	}
	_, _ = b.pool.Exec(ctx, `
		UPDATE discord_voice_runtime SET connected=true, last_disconnect_reason='', session_id=$2, voice_channel_id=$3 WHERE guild_id=$1`,
		guildID, sid, channelID)
	b.ensureStreamer(guildID)
	return nil
}

// LeaveGuild disconnects voice and marks discord_voice_runtime disconnected.
func (b *Bot) LeaveGuild(ctx context.Context, guildID string) error {
	if v, ok := b.voices.LoadAndDelete(guildID); ok {
		if gr, ok := v.(*guildRuntime); ok && gr.cancel != nil {
			gr.cancel()
		}
	}
	if sess := b.session(); sess != nil {
		if vc, ok := sess.VoiceConnections[guildID]; ok && vc != nil {
			disconnectVC(vc)
		}
	}
	_, _ = b.pool.Exec(ctx, `UPDATE discord_voice_runtime SET connected=false, last_disconnect_reason='leave' WHERE guild_id=$1`, guildID)
	return nil
}

func (b *Bot) ensureStreamer(guildID string) {
	if _, ok := b.voices.Load(guildID); ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	gr := &guildRuntime{cancel: cancel}
	if _, loaded := b.voices.LoadOrStore(guildID, gr); loaded {
		cancel()
		return
	}
	go b.streamLoop(ctx, guildID)
}

func (b *Bot) streamLoop(ctx context.Context, guildID string) {
	defer b.voices.Delete(guildID)
	var (
		trackCancel context.CancelFunc
		current     uuid.UUID
		status      string
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
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		var connected bool
		var reason *string
		_ = b.pool.QueryRow(ctx, `SELECT connected, last_disconnect_reason FROM discord_voice_runtime WHERE guild_id=$1`, guildID).Scan(&connected, &reason)
		if !connected {
			if reason != nil && *reason == "joining" {
				stopTrack()
				continue
			}
			stopTrack()
			if sess := b.session(); sess != nil {
				if vc, ok := sess.VoiceConnections[guildID]; ok && vc != nil {
					disconnectVC(vc)
				}
			}
			return
		}
		sid, err := b.play.Session(ctx, "discord_guild", guildID, nil)
		if err != nil {
			continue
		}
		st, err := b.play.Get(ctx, sid)
		if err != nil {
			continue
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
		if tid == current && stat == status && trackCancel != nil {
			continue
		}
		stopTrack()
		current, status = tid, stat
		tctx, cancel := context.WithCancel(ctx)
		trackCancel = cancel
		go b.playTrack(tctx, guildID, sid, tid, st)
	}
}

func (b *Bot) playTrack(ctx context.Context, guildID string, sid, trackID uuid.UUID, st map[string]any) {
	sess := b.session()
	if sess == nil {
		return
	}
	vc, ok := sess.VoiceConnections[guildID]
	if !ok || vc == nil {
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
		_ = b.play.Control(ctx, sid, "skip", nil)
		return
	}
	defer src.Close()

	pcmCmd, pcm, err := src.open(ctx, gainDB)
	if err != nil {
		b.recordPlaybackError(ctx, guildID, trackID, "ffmpeg", err.Error())
		_ = b.play.Control(ctx, sid, "skip", nil)
		return
	}
	defer func() {
		if pcm != nil {
			pcm.Close()
		}
		if pcmCmd != nil && pcmCmd.Process != nil {
			_ = pcmCmd.Process.Kill()
			_ = pcmCmd.Wait()
		}
	}()

	enc, err := startOpusEncoder(ctx, pcm)
	if err != nil {
		b.recordPlaybackError(ctx, guildID, trackID, "opus", err.Error())
		return
	}
	defer enc.Close()

	_ = vc.Speaking(true)
	defer vc.Speaking(false)

	started := time.Now()
	counted := false
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
		select {
		case <-ctx.Done():
			return
		case vc.OpusSend <- pkt:
		case <-time.After(2 * time.Second):
			return
		}
		if !counted && durationMS > 0 {
			elapsed := int(time.Since(started).Milliseconds())
			thresh := durationMS / 2
			if thresh > 30000 {
				thresh = 30000
			}
			if elapsed >= thresh {
				counted = true
				b.recordDiscordListens(ctx, guildID, trackID, durationMS)
			}
		}
	}
	if ctx.Err() != nil {
		return
	}
	// Natural end: advance the guild session.
	_ = b.play.Control(context.Background(), sid, "skip", nil)
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

func (s pcmSource) open(ctx context.Context, gainDB float64) (*exec.Cmd, io.ReadCloser, error) {
	if s.path != "" {
		return FFmpegPCM(ctx, s.path, gainDB)
	}
	if s.reader != nil {
		return ffmpegPCMReader(ctx, s.reader, gainDB)
	}
	return nil, nil, fmt.Errorf("no ffmpeg source")
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
	vol, _ := st["volume"].(float64)
	if vol <= 0 {
		vol = 1
	}
	mult := playback.ReplayGainMultiplier(mode, trackGain, albumGain, 0) * vol
	gainDB := 0.0
	if mult > 0 && mult != 1 {
		gainDB = 20 * math.Log10(mult)
	}
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

func (b *Bot) recordDiscordListens(ctx context.Context, guildID string, trackID uuid.UUID, durationMS int) {
	if b.scrobble == nil {
		return
	}
	rows, err := b.pool.Query(ctx, `
		SELECT i.user_id FROM discord_user_voice v
		JOIN user_identities i ON i.provider='discord' AND i.provider_user_id=v.discord_user_id
		WHERE v.guild_id=$1 AND v.channel_id IS NOT NULL AND v.channel_id <> ''`, guildID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var uid uuid.UUID
		if rows.Scan(&uid) != nil {
			continue
		}
		pos := durationMS
		if pos < 30000 {
			pos = 30000
		}
		_ = b.scrobble.HandleListen(ctx, uid, scrobble.Event{
			TrackID: trackID, PositionMS: pos, DurationMS: durationMS, Source: "discord", Kind: "progress",
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
		if !connected {
			if _, ok := b.voices.Load(gid); ok {
				_ = b.LeaveGuild(ctx, gid)
			}
		} else if ch != nil && *ch != "" {
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
			sid, err := b.play.Session(ctx, "discord_guild", gid, nil)
			if err == nil {
				items, _ := b.play.Queue(ctx, sid)
				st, _ := b.play.Get(ctx, sid)
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
