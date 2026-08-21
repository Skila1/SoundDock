package discordx

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (b *Bot) gatewayLoop(ctx context.Context) {
	var runCancel context.CancelFunc
	var current string
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			if runCancel != nil {
				runCancel()
			}
			b.closeSession()
			return
		case <-t.C:
		}
		enabled, token, _, _, err := b.loadSettings(ctx)
		if err != nil || !enabled || token == "" {
			if runCancel != nil {
				runCancel()
				runCancel = nil
				current = ""
			}
			b.closeSession()
			continue
		}
		if token == current && atomic.LoadInt32(&b.gwOn) == 1 {
			continue
		}
		if runCancel != nil {
			runCancel()
		}
		sctx, cancel := context.WithCancel(ctx)
		runCancel = cancel
		current = token
		atomic.StoreInt32(&b.gwOn, 1)
		go func(token string) {
			defer atomic.StoreInt32(&b.gwOn, 0)
			if err := b.runSession(sctx, token); err != nil && sctx.Err() == nil {
				b.log.Warn("discord gateway", "err", err)
				_, _ = b.pool.Exec(context.Background(), `UPDATE discord_settings SET last_gateway_status='error', last_error_redacted=$1 WHERE id=1`, redacted(err.Error()))
			}
		}(token)
	}
}

func (b *Bot) runSession(ctx context.Context, token string) error {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return err
	}
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates
	dg.StateEnabled = true
	dg.AddHandler(b.onReady)
	dg.AddHandler(b.onGuildCreate)
	dg.AddHandler(b.onVoiceState)
	dg.AddHandler(b.onInteraction)
	if err := dg.Open(); err != nil {
		return err
	}
	b.setSession(dg)
	defer func() {
		b.setSession(nil)
		_ = dg.Close()
	}()
	<-ctx.Done()
	return nil
}

func (b *Bot) setSession(s *discordgo.Session) {
	b.sessMu.Lock()
	b.sess = s
	b.sessMu.Unlock()
}

func (b *Bot) session() *discordgo.Session {
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	return b.sess
}

func (b *Bot) closeSession() {
	b.sessMu.Lock()
	s := b.sess
	b.sess = nil
	b.sessMu.Unlock()
	if s != nil {
		_ = s.Close()
	}
}

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	b.log.Info("discord gateway ready", "user", r.User.Username)
	_, _ = b.pool.Exec(context.Background(), `UPDATE discord_settings SET last_gateway_status='connected', last_error_redacted=NULL WHERE id=1`)
	go func() {
		ctx := context.Background()
		enabled, token, appID, _, err := b.loadSettings(ctx)
		if err != nil || !enabled || token == "" {
			return
		}
		if err := b.syncCommands(ctx, token, appID); err != nil {
			b.log.Warn("command sync on ready", "err", err)
		} else {
			_, _ = b.pool.Exec(ctx, `UPDATE discord_settings SET command_registration_status='ok' WHERE id=1`)
		}
	}()
}

func (b *Bot) onGuildCreate(s *discordgo.Session, g *discordgo.GuildCreate) {
	if g.Guild == nil {
		return
	}
	ctx := context.Background()
	_, _ = b.pool.Exec(ctx, `INSERT INTO discord_guilds (id, name) VALUES ($1,$2) ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name`, g.ID, g.Name)
	enabled, token, appID, _, err := b.loadSettings(ctx)
	if err != nil || !enabled || token == "" || appID == nil || *appID == "" {
		return
	}
	body, _ := marshalCommands()
	u := "https://discord.com/api/v10/applications/" + *appID + "/guilds/" + g.ID + "/commands"
	if err := putDiscordJSON(ctx, token, u, body); err != nil {
		b.log.Warn("guild command register", "guild", g.ID, "err", err)
	}
}

func (b *Bot) onVoiceState(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
	if vs == nil || vs.VoiceState == nil {
		return
	}
	ctx := context.Background()
	uid := vs.UserID
	gid := vs.GuildID
	ch := vs.ChannelID
	if ch == "" {
		_, _ = b.pool.Exec(ctx, `DELETE FROM discord_user_voice WHERE discord_user_id=$1 AND guild_id=$2`, uid, gid)
		return
	}
	_, _ = b.pool.Exec(ctx, `
		INSERT INTO discord_user_voice (discord_user_id, guild_id, channel_id, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (discord_user_id, guild_id) DO UPDATE SET channel_id=EXCLUDED.channel_id, updated_at=now()`,
		uid, gid, ch)
}

// VoiceOfUser returns the guild and channel the Discord user is actually in.
// If they are in several bot guilds, the most recently updated VC wins.
func (b *Bot) VoiceOfUser(discordUserID string) (guildID, channelID string, ok bool) {
	if s := b.session(); s != nil && s.State != nil {
		type hit struct {
			g, c string
		}
		var hits []hit
		for _, g := range s.State.Guilds {
			st, err := s.State.VoiceState(g.ID, discordUserID)
			if err == nil && st != nil && st.ChannelID != "" {
				hits = append(hits, hit{g.ID, st.ChannelID})
			}
		}
		if len(hits) == 1 {
			return hits[0].g, hits[0].c, true
		}
		if len(hits) > 1 {
			// Prefer a VC recorded most recently in discord_user_voice.
			var g, c string
			err := b.pool.QueryRow(context.Background(), `
				SELECT guild_id, channel_id FROM discord_user_voice
				WHERE discord_user_id=$1 AND channel_id IS NOT NULL AND channel_id <> ''
				ORDER BY updated_at DESC LIMIT 1`, discordUserID).Scan(&g, &c)
			if err == nil && c != "" {
				return g, c, true
			}
			return hits[0].g, hits[0].c, true
		}
	}
	var g, c string
	err := b.pool.QueryRow(context.Background(), `
		SELECT guild_id, channel_id FROM discord_user_voice
		WHERE discord_user_id=$1 AND channel_id IS NOT NULL AND channel_id <> ''
		ORDER BY updated_at DESC LIMIT 1`, discordUserID).Scan(&g, &c)
	if err != nil || c == "" {
		return "", "", false
	}
	return g, c, true
}

func (b *Bot) ApplicationID(ctx context.Context) string {
	var id *string
	_ = b.pool.QueryRow(ctx, `SELECT coalesce(application_id, client_id) FROM discord_settings WHERE id=1`).Scan(&id)
	if id == nil {
		return ""
	}
	return *id
}

func (b *Bot) DiscordEnabled(ctx context.Context) bool {
	var en bool
	var enc []byte
	_ = b.pool.QueryRow(ctx, `SELECT enabled, bot_token_enc FROM discord_settings WHERE id=1`).Scan(&en, &enc)
	return en && len(enc) > 0
}
