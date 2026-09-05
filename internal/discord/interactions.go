package discordx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/playback"
)

func (b *Bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommandAutocomplete:
		b.handleAutocomplete(s, i)
	case discordgo.InteractionApplicationCommand:
		b.handleCommand(s, i)
	}
}

func interactionUser(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func optionString(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, o := range opts {
		if o.Name == name {
			return o.StringValue()
		}
	}
	if len(opts) > 0 && opts[0].Type == discordgo.ApplicationCommandOptionString {
		return opts[0].StringValue()
	}
	return ""
}

func optionInt(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) int {
	for _, o := range opts {
		if o.Name == name {
			return int(o.IntValue())
		}
	}
	return 0
}

func (b *Bot) reply(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: msg, Flags: discordgo.MessageFlagsEphemeral},
	})
}

func (b *Bot) replyPublic(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: msg},
	})
}

func (b *Bot) deferReply(s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
}

func (b *Bot) deferReplyEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
}

func (b *Bot) followup(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: msg})
}

func (b *Bot) followupEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: msg,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
}

func (b *Bot) handleAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	q := optionString(data.Options, "query")
	if q == "" && len(data.Options) > 0 {
		q = data.Options[0].StringValue()
	}
	libs := b.guildLibraries(context.Background(), i.GuildID)
	hits, err := b.search.Suggest(context.Background(), q, libs, 8)
	if err != nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionApplicationCommandAutocompleteResult,
			Data: &discordgo.InteractionResponseData{Choices: []*discordgo.ApplicationCommandOptionChoice{}},
		})
		return
	}
	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, h := range hits {
		if h.Type != "track" && h.Type != "album" && h.Type != "playlist" {
			continue
		}
		name := h.Title
		if h.Artist != "" {
			name = h.Title + " - " + h.Artist
		}
		if len(name) > 100 {
			name = name[:100]
		}
		val := h.Title
		if h.Artist != "" {
			val = h.Title + " " + h.Artist
		}
		if len(val) > 100 {
			val = val[:100]
		}
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: val})
		if len(choices) >= 25 {
			break
		}
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	})
}

func (b *Bot) handleCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.GuildID == "" {
		b.reply(s, i, "Use this command in a server.")
		return
	}
	if !b.guildEnabled(context.Background(), i.GuildID) {
		b.reply(s, i, "SoundDock is disabled in this server.")
		return
	}
	data := i.ApplicationCommandData()
	user := interactionUser(i)
	ctx := context.Background()
	switch data.Name {
	case "join":
		g, ch, ok := b.VoiceOfUser(user)
		if !ok || ch == "" {
			b.reply(s, i, "Join a voice channel first.")
			return
		}
		if g != i.GuildID {
			b.reply(s, i, "Join a voice channel in this server first.")
			return
		}
		if botCh, ok := b.BotChannel(i.GuildID); ok && botCh != ch {
			b.reply(s, i, "I am already in another voice channel. Join that one, or use /leave first.")
			return
		}
		if err := b.JoinChannel(ctx, i.GuildID, ch); err != nil {
			b.reply(s, i, "Could not join: "+err.Error())
			return
		}
		b.reply(s, i, "Joined your voice channel.")
	case "leave":
		_ = b.LeaveGuild(ctx, i.GuildID)
		b.reply(s, i, "Left voice.")
	case "play":
		q := optionString(data.Options, "query")
		if strings.TrimSpace(q) == "" {
			b.reply(s, i, "Give a SoundDock search query.")
			return
		}
		g, ch, ok := b.VoiceOfUser(user)
		if !ok || ch == "" {
			b.reply(s, i, "Join a voice channel first.")
			return
		}
		if g != i.GuildID {
			b.reply(s, i, "Join a voice channel in this server first.")
			return
		}
		if botCh, ok := b.BotChannel(i.GuildID); ok && botCh != ch {
			b.reply(s, i, "Join the voice channel I am already in, or use /leave first.")
			return
		}
		b.deferReply(s, i)
		if err := b.JoinChannel(ctx, i.GuildID, ch); err != nil {
			b.followup(s, i, "Could not join: "+err.Error())
			return
		}
		if err := b.PlayQuery(ctx, i.GuildID, user, q, b.guildLibraries(ctx, i.GuildID)); err != nil {
			b.followup(s, i, err.Error())
			return
		}
		b.followup(s, i, "Playing from SoundDock: **"+q+"**")
	case "search":
		q := optionString(data.Options, "query")
		b.deferReply(s, i)
		hits, err := b.search.Search(ctx, q, []string{"track", "album", "playlist"}, b.guildLibraries(ctx, i.GuildID), 8)
		if err != nil || len(hits) == 0 {
			b.followup(s, i, "No SoundDock library matches.")
			return
		}
		var bld strings.Builder
		bld.WriteString("SoundDock results:\n")
		for n, h := range hits {
			if n >= 8 {
				break
			}
			fmt.Fprintf(&bld, "%d. [%s] %s", n+1, h.Type, h.Title)
			if h.Artist != "" {
				fmt.Fprintf(&bld, " - %s", h.Artist)
			}
			bld.WriteByte('\n')
		}
		b.followup(s, i, bld.String())
	case "pause":
		b.sessionControl(ctx, s, i, "pause", nil, "Paused.")
	case "resume":
		b.sessionControl(ctx, s, i, "resume", nil, "Resumed.")
	case "skip":
		b.sessionControl(ctx, s, i, "skip", nil, "Skipped.")
	case "previous":
		b.sessionControl(ctx, s, i, "previous", nil, "Previous track.")
	case "stop":
		b.deferReplyEphemeral(s, i)
		sid, err := b.ensureBoundSession(ctx, i.GuildID, b.voiceChannelForGuild(i.GuildID))
		if err != nil {
			b.followupEphemeral(s, i, err.Error())
			return
		}
		_ = b.play.Control(ctx, sid, "stop", extraWithCommandID(nil, i.ID))
		_ = b.play.Control(ctx, sid, "clear", map[string]any{"all": true})
		b.followupEphemeral(s, i, "Stopped and cleared.")
	case "clear":
		b.sessionControl(ctx, s, i, "clear", nil, "Queue cleared.")
	case "shuffle":
		b.sessionControl(ctx, s, i, "shuffle", nil, "Shuffle toggled.")
	case "repeat":
		b.deferReplyEphemeral(s, i)
		sid, err := b.ensureBoundSession(ctx, i.GuildID, b.voiceChannelForGuild(i.GuildID))
		if err != nil {
			b.followupEphemeral(s, i, err.Error())
			return
		}
		st, _ := b.play.Get(ctx, sid)
		mode, _ := st["repeat"].(string)
		next := nextRepeatMode(mode)
		if err := b.play.Control(ctx, sid, "repeat", extraWithCommandID(map[string]any{"mode": next}, i.ID)); err != nil {
			b.followupEphemeral(s, i, err.Error())
			return
		}
		b.followupEphemeral(s, i, "Repeat: **"+repeatModeLabel(next)+"**")
	case "volume":
		level := optionInt(data.Options, "level")
		vol := float64(level) / 100
		b.sessionControl(ctx, s, i, "volume", map[string]any{"volume": vol}, fmt.Sprintf("Volume %d%%", level))
	case "queue":
		sid, err := b.ensureBoundSession(ctx, i.GuildID, b.voiceChannelForGuild(i.GuildID))
		if err != nil {
			b.reply(s, i, err.Error())
			return
		}
		items, _ := b.play.Queue(ctx, sid)
		st, _ := b.play.Get(ctx, sid)
		idx, _ := st["current_index"].(int)
		if len(items) == 0 {
			b.reply(s, i, "Queue is empty.")
			return
		}
		var bld strings.Builder
		bld.WriteString("Queue:\n")
		for n, it := range items {
			if n >= 15 {
				fmt.Fprintf(&bld, "… %d more\n", len(items)-15)
				break
			}
			mark := " "
			if n == idx {
				mark = "▶"
			}
			title := trackTitle(ctx, b, fmt.Sprint(it["track_id"]))
			fmt.Fprintf(&bld, "%s %d. %s\n", mark, n+1, title)
		}
		b.replyPublic(s, i, bld.String())
	case "nowplaying":
		sid, err := b.ensureBoundSession(ctx, i.GuildID, b.voiceChannelForGuild(i.GuildID))
		if err != nil {
			b.reply(s, i, err.Error())
			return
		}
		st, _ := b.play.Get(ctx, sid)
		cur := fmt.Sprint(st["current_track_id"])
		if cur == "" || cur == "<nil>" {
			b.reply(s, i, "Nothing playing.")
			return
		}
		title := trackTitle(ctx, b, cur)
		b.replyPublic(s, i, "Now playing: **"+title+"**")
	case "link":
		b.handleLink(s, i, user)
	case "playlist":
		b.handlePlaylist(s, i, data.Options, user)
	default:
		b.reply(s, i, "Unknown command.")
	}
}

// engineRepeatMode maps Discord-facing names onto playback.Engine values (off|queue|one).
func engineRepeatMode(mode string) string {
	if mode == "track" {
		return "one"
	}
	return mode
}

func nextRepeatMode(current string) string {
	switch engineRepeatMode(current) {
	case "off", "":
		return "one"
	case "one":
		return "queue"
	case "queue":
		return "off"
	default:
		return "queue"
	}
}

func repeatModeLabel(mode string) string {
	if mode == "one" {
		return "track"
	}
	return mode
}

func (b *Bot) sessionControl(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, action string, extra map[string]any, ok string) {
	b.deferReplyEphemeral(s, i)
	sid, err := b.ensureBoundSession(ctx, i.GuildID, b.voiceChannelForGuild(i.GuildID))
	if err != nil {
		b.followupEphemeral(s, i, err.Error())
		return
	}
	if err := b.claimDiscordForCommand(ctx, sid); err != nil && !errors.Is(err, playback.ErrLeaseConflict) {
		b.followupEphemeral(s, i, err.Error())
		return
	}
	if err := b.play.Control(ctx, sid, action, extraWithCommandID(extra, i.ID)); err != nil {
		b.followupEphemeral(s, i, err.Error())
		return
	}
	b.followupEphemeral(s, i, ok)
}

func trackTitle(ctx context.Context, b *Bot, id string) string {
	id = strings.TrimSpace(strings.Trim(id, "<>"))
	if id == "" || id == "nil" {
		return "(unknown)"
	}
	var title, artist string
	err := b.pool.QueryRow(ctx, `
		SELECT t.title, coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
			FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
			WHERE ta.track_id=t.id AND ta.role='primary'),'')
		FROM tracks t WHERE t.id::text=$1`, id).Scan(&title, &artist)
	if err != nil {
		return id
	}
	if artist != "" {
		return title + " - " + artist
	}
	return title
}

func (b *Bot) handleLink(s *discordgo.Session, i *discordgo.InteractionCreate, discordUser string) {
	ctx := context.Background()
	if uid, ok := b.linkedUserID(ctx, discordUser); ok {
		b.reply(s, i, "Already linked to a SoundDock account (`"+uid.String()+"`).")
		return
	}
	tok, err := cryptox.RandomToken(24)
	if err != nil {
		b.reply(s, i, "Could not start link.")
		return
	}
	hash := cryptox.HashToken(tok)
	uname := ""
	if i.Member != nil && i.Member.User != nil {
		uname = i.Member.User.Username
	}
	_, _ = b.pool.Exec(ctx, `
		INSERT INTO identity_link_challenges (token_hash, provider, provider_user_id, provider_username, expires_at)
		VALUES ($1,'discord',$2,$3,now()+interval '15 minutes')`, hash, discordUser, uname)
	base := b.public
	if base == "" {
		base = strings.TrimRight(os.Getenv("SD_PUBLIC_URL"), "/")
	}
	if base == "" {
		b.reply(s, i, "Open SoundDock in the browser → Connected Services and link Discord. Never enter your SoundDock password here.")
		return
	}
	b.reply(s, i, "Link your account in the browser (never type your password in Discord):\n"+base+"/settings/connected?discord_link="+tok)
}

func (b *Bot) handlePlaylist(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption, user string) {
	ctx := context.Background()
	if len(opts) == 0 {
		b.reply(s, i, "Use `/playlist list` or `/playlist play`.")
		return
	}
	sub := opts[0]
	uid, linked := b.linkedUserID(ctx, user)
	switch sub.Name {
	case "list":
		if !linked {
			b.reply(s, i, "Link your SoundDock account with `/link` first.")
			return
		}
		rows, err := b.pool.Query(ctx, `SELECT name FROM playlists WHERE user_id=$1 OR public=true ORDER BY updated_at DESC LIMIT 20`, uid)
		if err != nil {
			b.reply(s, i, err.Error())
			return
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			b.reply(s, i, "No playlists.")
			return
		}
		b.reply(s, i, "Playlists:\n• "+strings.Join(names, "\n• "))
	case "play":
		name := optionString(sub.Options, "name")
		if name == "" {
			b.reply(s, i, "Give a playlist name.")
			return
		}
		g, ch, ok := b.VoiceOfUser(user)
		if !ok || g != i.GuildID {
			b.reply(s, i, "Join a voice channel in this server first.")
			return
		}
		b.deferReply(s, i)
		if err := b.JoinChannel(ctx, i.GuildID, ch); err != nil {
			b.followup(s, i, err.Error())
			return
		}
		var pid interface{}
		q := `SELECT id FROM playlists WHERE lower(name)=lower($1)`
		args := []any{name}
		if linked {
			q += ` AND (user_id=$2 OR public=true) LIMIT 1`
			args = append(args, uid)
		} else {
			q += ` AND public=true LIMIT 1`
		}
		if err := b.pool.QueryRow(ctx, q, args...).Scan(&pid); err != nil {
			b.followup(s, i, "Playlist not found in SoundDock.")
			return
		}
		if err := b.PlayQuery(ctx, i.GuildID, user, name, b.guildLibraries(ctx, i.GuildID)); err != nil {
			b.followup(s, i, err.Error())
			return
		}
		b.followup(s, i, "Playing playlist **"+name+"**")
	default:
		b.reply(s, i, "Unknown playlist subcommand.")
	}
}
