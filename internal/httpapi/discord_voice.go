package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	discordx "github.com/sounddock/sounddock/internal/discord"
)

var (
	errNotInVoice    = errors.New("not in a voice channel")
	errGuildDisabled = errors.New("this Discord server is disabled")
)

// MountP7 registers Discord voice-output and scrobble routes on an authenticated /api/v1 router.
func (s *Server) MountP7(r chi.Router) {
	r.Get("/me/discord/voice-state", s.discordVoiceState)
	r.Post("/me/discord/join", s.discordJoin)
	r.Post("/me/discord/play", s.discordPlay)
	r.Post("/me/discord/link", s.discordCompleteLink)
	r.Post("/me/listen", s.postListen)
	r.Get("/me/scrobble", s.getScrobble)
	r.Put("/me/scrobble", s.putScrobble)
	r.Post("/me/scrobble/import", s.importScrobbleHistory)
}

func (s *Server) discordUserID(r *http.Request) string {
	var id string
	_ = s.Pool.QueryRow(r.Context(), `SELECT provider_user_id FROM user_identities WHERE user_id=$1 AND provider='discord'`, currentUser(r).ID).Scan(&id)
	return id
}

func (s *Server) discordVoiceState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	enabled := false
	var enc []byte
	var appID *string
	_ = s.Pool.QueryRow(ctx, `SELECT enabled, bot_token_enc, coalesce(application_id, client_id) FROM discord_settings WHERE id=1`).Scan(&enabled, &enc, &appID)
	discordEnabled := enabled && len(enc) > 0
	did := s.discordUserID(r)
	linked := did != ""
	inVoice := false
	var guildID, channelID string
	if linked {
		if bot := discordx.Live(); bot != nil {
			if g, c, ok := bot.VoiceOfUser(did); ok {
				inVoice, guildID, channelID = true, g, c
			}
		}
		if !inVoice {
			_ = s.Pool.QueryRow(ctx, `
				SELECT guild_id, channel_id FROM discord_user_voice
				WHERE discord_user_id=$1 AND channel_id IS NOT NULL AND channel_id <> ''
				ORDER BY updated_at DESC LIMIT 1`, did).Scan(&guildID, &channelID)
			inVoice = channelID != ""
		}
	}
	out := map[string]any{
		"discord_enabled": discordEnabled,
		"linked":          linked,
		"in_voice":        inVoice,
		"guild_id":        nil,
		"channel_id":      nil,
	}
	if inVoice {
		out["guild_id"] = guildID
		out["channel_id"] = channelID
	}
	if appID != nil && *appID != "" {
		out["application_id"] = *appID
	}
	writeJSON(w, 200, out)
}

func (s *Server) findUserVoice(r *http.Request) (guildID, channelID string, ok bool) {
	did := s.discordUserID(r)
	if did == "" {
		return "", "", false
	}
	if bot := discordx.Live(); bot != nil {
		if g, c, found := bot.VoiceOfUser(did); found {
			return g, c, true
		}
	}
	var g, c string
	err := s.Pool.QueryRow(r.Context(), `
		SELECT guild_id, channel_id FROM discord_user_voice
		WHERE discord_user_id=$1 AND channel_id IS NOT NULL AND channel_id <> ''
		ORDER BY updated_at DESC LIMIT 1`, did).Scan(&g, &c)
	if err != nil || c == "" {
		return "", "", false
	}
	return g, c, true
}

func (s *Server) discordPlaySession(r *http.Request) (uuid.UUID, error) {
	g, _, ok := s.findUserVoice(r)
	if !ok {
		return uuid.Nil, errNotInVoice
	}
	if !s.guildPlaybackEnabled(r.Context(), g) {
		return uuid.Nil, errGuildDisabled
	}
	u := currentUser(r)
	return s.Play.Session(r.Context(), "discord_guild", g, &u.ID)
}

func (s *Server) guildPlaybackEnabled(ctx context.Context, guildID string) bool {
	var en bool
	err := s.Pool.QueryRow(ctx, `SELECT enabled FROM discord_guilds WHERE id=$1`, guildID).Scan(&en)
	if err != nil {
		return true
	}
	return en
}

func (s *Server) discordJoin(w http.ResponseWriter, r *http.Request) {
	g, c, ok := s.findUserVoice(r)
	if !ok {
		writeErr(w, 409, "not_in_voice", "not in a voice channel")
		return
	}
	if err := s.ensureDiscordJoin(r, g, c); err != nil {
		if errors.Is(err, errGuildDisabled) {
			writeErr(w, 403, "guild_disabled", err.Error())
			return
		}
		writeErr(w, 500, "voice", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "guild_id": g, "channel_id": c})
}

func (s *Server) ensureDiscordJoin(r *http.Request, guildID, channelID string) error {
	ctx := r.Context()
	if !s.guildPlaybackEnabled(ctx, guildID) {
		return errGuildDisabled
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO discord_guilds (id) VALUES ($1) ON CONFLICT DO NOTHING`, guildID)
	sid, err := s.Play.Session(ctx, "discord_guild", guildID, &currentUser(r).ID)
	if err != nil {
		return err
	}
	if bot := discordx.Live(); bot != nil {
		return bot.JoinChannel(ctx, guildID, channelID)
	}
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO discord_voice_runtime (guild_id, voice_channel_id, session_id, connected, last_disconnect_reason)
		VALUES ($1,$2,$3,false,'pending_join')
		ON CONFLICT (guild_id) DO UPDATE SET voice_channel_id=$2, session_id=$3, connected=false, last_disconnect_reason='pending_join'`,
		guildID, channelID, sid)
	return nil
}

func (s *Server) discordPlay(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackIDs []string `json:"track_ids"`
		Start    int      `json:"start"`
	}
	_ = decodeJSON(r, &body)
	g, c, ok := s.findUserVoice(r)
	if !ok {
		writeErr(w, 409, "not_in_voice", "not in a voice channel")
		return
	}
	if err := s.ensureDiscordJoin(r, g, c); err != nil {
		if errors.Is(err, errGuildDisabled) {
			writeErr(w, 403, "guild_disabled", err.Error())
			return
		}
		writeErr(w, 500, "voice", err.Error())
		return
	}
	u := currentUser(r)
	sid, err := s.Play.Session(r.Context(), "discord_guild", g, &u.ID)
	if err != nil {
		writeErr(w, 500, "queue", err.Error())
		return
	}
	ids, err := s.resolveQueueTracks(r.Context(), body.TrackIDs)
	if err != nil {
		writeErr(w, 502, "scapex", err.Error())
		return
	}
	if err := s.Play.Replace(r.Context(), sid, ids, body.Start); err != nil {
		writeErr(w, 500, "queue", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "guild_id": g, "channel_id": c, "session_id": sid})
}

func (s *Server) discordCompleteLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Challenge string `json:"challenge"`
	}
	_ = decodeJSON(r, &body)
	if body.Challenge == "" {
		writeErr(w, 400, "invalid", "challenge required")
		return
	}
	hash := cryptox.HashToken(body.Challenge)
	var did, uname string
	err := s.Pool.QueryRow(r.Context(), `
		SELECT coalesce(provider_user_id,''), coalesce(provider_username,'')
		FROM identity_link_challenges
		WHERE token_hash=$1 AND provider='discord' AND consumed_at IS NULL AND expires_at>now()`, hash).
		Scan(&did, &uname)
	if err != nil || did == "" {
		writeErr(w, 400, "invalid", "expired or invalid challenge")
		return
	}
	u := currentUser(r)
	_, err = s.Pool.Exec(r.Context(), `
		INSERT INTO user_identities (user_id, provider, provider_user_id, provider_username)
		VALUES ($1,'discord',$2,$3)
		ON CONFLICT (provider, provider_user_id) DO UPDATE SET user_id=EXCLUDED.user_id, provider_username=EXCLUDED.provider_username`,
		u.ID, did, uname)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE identity_link_challenges SET consumed_at=now(), user_id=$2 WHERE token_hash=$1`, hash, u.ID)
	if s.Audit != nil {
		s.Audit.Event(r.Context(), &u.ID, "identity.link", "discord", r.RemoteAddr, nil)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
