package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	discordx "github.com/sounddock/sounddock/internal/discord"
	"github.com/sounddock/sounddock/internal/minilib"
	"github.com/sounddock/sounddock/internal/playback"
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

// attachedBoundSession returns runtime.session_id when the caller has a verified
// Discord identity and is in the bot's bound voice channel. Does not copy queues.
func (s *Server) attachedBoundSession(r *http.Request) (uuid.UUID, bool) {
	did := s.discordUserID(r)
	if did == "" {
		return uuid.Nil, false
	}
	guildID, channelID := "", ""
	if bot := discordx.Live(); bot != nil {
		if g, c, found := bot.VoiceOfUser(did); found {
			guildID, channelID = g, c
		}
	}
	if channelID == "" {
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT guild_id, channel_id FROM discord_user_voice
			WHERE discord_user_id=$1 AND channel_id IS NOT NULL AND channel_id <> ''
			ORDER BY updated_at DESC LIMIT 1`, did).Scan(&guildID, &channelID)
	}
	if guildID == "" || channelID == "" {
		return uuid.Nil, false
	}
	if bot := discordx.Live(); bot != nil {
		if live, ok := bot.BotChannel(guildID); !ok || live != channelID {
			return uuid.Nil, false
		}
	}
	var sid uuid.UUID
	var runtimeCh *string
	var connected bool
	var reason string
	err := s.Pool.QueryRow(r.Context(), `
		SELECT session_id, voice_channel_id, connected, coalesce(last_disconnect_reason,'')
		FROM discord_voice_runtime
		WHERE guild_id=$1 AND session_id IS NOT NULL`, guildID).Scan(&sid, &runtimeCh, &connected, &reason)
	if err != nil || sid == uuid.Nil {
		return uuid.Nil, false
	}
	if runtimeCh == nil || *runtimeCh == "" || *runtimeCh != channelID {
		return uuid.Nil, false
	}
	if connected || reason == "pending_join" || reason == "joining" {
		return sid, true
	}
	return uuid.Nil, false
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
	var body struct {
		ExpectedBindingRevision int64  `json:"expected_binding_revision"`
		RendererID              string `json:"renderer_id"`
		RendererGeneration      int64  `json:"renderer_generation"`
		DeviceID                string `json:"device_id"`
	}
	_ = decodeJSON(r, &body)
	g, c, ok := s.findUserVoice(r)
	if !ok {
		writeErr(w, 409, "not_in_voice", "not in a voice channel")
		return
	}
	sid, err := s.attachedPlaySession(r, nil, body.DeviceID)
	if s.writePlaySessionErr(w, err) {
		return
	}
	res, err := s.ensureDiscordJoin(r, g, c, sid, body.ExpectedBindingRevision, body.RendererID, body.RendererGeneration)
	if s.writePlaySessionErr(w, err) {
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "guild_id": g, "channel_id": c, "session_id": sid,
		"binding_revision": res.BindingRevision, "state_revision": res.StateRevision,
	})
}

func (s *Server) ensureDiscordJoin(r *http.Request, guildID, channelID string, sid uuid.UUID, expectedBindingRevision int64, _ string, _ int64) (playback.BindResult, error) {
	ctx := r.Context()
	if !s.guildPlaybackEnabled(ctx, guildID) {
		return playback.BindResult{}, errGuildDisabled
	}
	if bot := discordx.Live(); bot != nil {
		if err := bot.JoinChannel(ctx, guildID, channelID); err != nil {
			return playback.BindResult{}, err
		}
	}
	res, err := s.Play.BindGuildSession(ctx, guildID, sid, channelID, expectedBindingRevision)
	if err != nil {
		return playback.BindResult{}, err
	}
	if discordx.Live() == nil {
		_, _ = s.Pool.Exec(ctx, `
			UPDATE discord_voice_runtime
			SET last_disconnect_reason='pending_join'
			WHERE guild_id=$1 AND connected=false`, guildID)
	}
	return res, nil
}

func (s *Server) discordPlay(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackIDs                []string `json:"track_ids"`
		Start                   int      `json:"start"`
		DeviceID                string   `json:"device_id"`
		ExpectedBindingRevision int64    `json:"expected_binding_revision"`
		RendererID              string   `json:"renderer_id"`
		RendererGeneration      int64    `json:"renderer_generation"`
	}
	_ = decodeJSON(r, &body)
	g, c, ok := s.findUserVoice(r)
	if !ok {
		writeErr(w, 409, "not_in_voice", "not in a voice channel")
		return
	}
	sid, err := s.attachedPlaySession(r, nil, body.DeviceID)
	if s.writePlaySessionErr(w, err) {
		return
	}
	res, err := s.ensureDiscordJoin(r, g, c, sid, body.ExpectedBindingRevision, body.RendererID, body.RendererGeneration)
	if s.writePlaySessionErr(w, err) {
		return
	}
	if len(body.TrackIDs) > 0 {
		ids, err := s.resolveQueueTracks(r.Context(), body.TrackIDs)
		if err != nil {
			writeErr(w, 502, "scapex", err.Error())
			return
		}
		if err := s.Play.Replace(s.withQueueRequester(r), sid, ids, body.Start); err != nil {
			writeErr(w, 400, "queue", err.Error())
			return
		}
		if q, gerr := s.Play.Get(r.Context(), sid); gerr == nil {
			if rev, ok := anyInt64(q["state_revision"]); ok {
				res.StateRevision = rev
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "guild_id": g, "channel_id": c, "session_id": sid,
		"binding_revision": res.BindingRevision, "state_revision": res.StateRevision,
	})
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
	var existing uuid.UUID
	err = s.Pool.QueryRow(r.Context(), `
		SELECT user_id FROM user_identities WHERE provider='discord' AND provider_user_id=$1`, did).Scan(&existing)
	if err == nil && existing != u.ID {
		writeErr(w, 409, "identity_taken", "this Discord account is already linked")
		return
	}
	_, err = s.Pool.Exec(r.Context(), `
		INSERT INTO user_identities (user_id, provider, provider_user_id, provider_username)
		VALUES ($1,'discord',$2,$3)
		ON CONFLICT (provider, provider_user_id) DO UPDATE SET provider_username=EXCLUDED.provider_username
		WHERE user_identities.user_id=EXCLUDED.user_id`,
		u.ID, did, uname)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE identity_link_challenges SET consumed_at=now(), user_id=$2 WHERE token_hash=$1`, hash, u.ID)
	_ = minilib.Reconcile(r.Context(), s.Pool, u.ID, did)
	if s.Audit != nil {
		s.Audit.Event(r.Context(), &u.ID, "identity.link", "discord", r.RemoteAddr, nil)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
