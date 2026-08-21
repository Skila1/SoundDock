package httpapi

import (
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/external"
)

func (s *Server) meProviders(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.Pool.Query(r.Context(), `
		SELECT s.provider, s.enabled, s.users_may_connect, s.public_import,
			a.display_name, a.status, a.last_successful_sync_at, a.connected_at, a.last_error, a.scopes
		FROM external_provider_settings s
		LEFT JOIN external_provider_accounts a ON a.provider=s.provider AND a.user_id=$1
		ORDER BY s.provider`, u.ID)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var prov string
		var enabled, may, pub bool
		var name, status, lastErr *string
		var lastSync, connected *time.Time
		var scopes []string
		if err := rows.Scan(&prov, &enabled, &may, &pub, &name, &status, &lastSync, &connected, &lastErr, &scopes); err != nil {
			continue
		}
		m := map[string]any{
			"provider": prov, "enabled": enabled, "users_may_connect": may, "public_import": pub,
			"capabilities": external.Caps[prov], "connected": status != nil && *status == "connected",
		}
		if name != nil {
			m["account_name"] = *name
		}
		if status != nil {
			m["status"] = *status
		}
		if lastSync != nil {
			m["last_successful_sync_at"] = lastSync
		}
		if connected != nil {
			m["connected_at"] = connected
		}
		if lastErr != nil && *lastErr != "" {
			m["last_error"] = *lastErr
		}
		if len(scopes) > 0 {
			m["scopes"] = scopes
		}
		out = append(out, m)
	}
	writeJSON(w, 200, out)
}

func (s *Server) connectProvider(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !auth.HasPerm(u, "providers.connect") {
		writeErr(w, 403, "forbidden", "not permitted")
		return
	}
	prov := chi.URLParam(r, "provider")
	if !external.Known(prov) {
		writeErr(w, 404, "not_found", "unknown provider")
		return
	}
	st, err := external.LoadSettings(r.Context(), s.Pool, s.Box, prov)
	if err != nil || !st.Enabled || !st.UsersMayConnect {
		writeErr(w, 400, "disabled", "provider is not enabled")
		return
	}
	if prov == "apple_music" {
		var body struct {
			MusicUserToken string `json:"music_user_token"`
			Name           string `json:"name"`
		}
		_ = decodeJSON(r, &body)
		if body.MusicUserToken == "" {
			writeErr(w, 400, "invalid", "music_user_token required")
			return
		}
		err := external.StoreAccount(r.Context(), s.Pool, s.Box, u.ID, prov, external.Token{
			Access: body.MusicUserToken, Name: body.Name, AccountID: "apple",
		})
		if err != nil {
			writeErr(w, 500, "db", err.Error())
			return
		}
		if s.Hooks != nil {
			s.Hooks.Emit(r.Context(), "external.provider.connected", map[string]any{"provider": prov, "user_id": u.ID})
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if st.ClientID == "" {
		writeErr(w, 400, "not_configured", "administrator has not configured this provider")
		return
	}
	redir := external.CallbackURL(s.absURL(r), prov)
	state := external.NewState()
	ver, ch := external.PKCE()
	var enc []byte
	if s.Box != nil {
		enc, _ = s.Box.Encrypt([]byte(ver))
	}
	_, err = s.Pool.Exec(r.Context(), `INSERT INTO oauth_transactions (state, provider, user_id, code_verifier_enc, redirect_uri, expires_at) VALUES ($1,$2,$3,$4,$5,now()+interval '15 minutes')`,
		state, prov, u.ID, enc, redir)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"url": external.AuthURL(prov, st.ClientID, redir, state, ch)})
}

func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	prov := chi.URLParam(r, "provider")
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	errQ := r.URL.Query().Get("error")
	fail := func(msg string) {
		http.Redirect(w, r, s.absURL(r)+"/settings/connected?error="+url.QueryEscape(msg), http.StatusFound)
	}
	if errQ != "" || code == "" || state == "" {
		fail("oauth_denied")
		return
	}
	var uid uuid.UUID
	var verEnc []byte
	var redir string
	err := s.Pool.QueryRow(r.Context(), `SELECT user_id, code_verifier_enc, redirect_uri FROM oauth_transactions WHERE state=$1 AND provider=$2 AND expires_at>now()`, state, prov).
		Scan(&uid, &verEnc, &redir)
	if err != nil {
		fail("invalid_state")
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM oauth_transactions WHERE state=$1`, state)
	ver := ""
	if s.Box != nil && len(verEnc) > 0 {
		if p, e := s.Box.Decrypt(verEnc); e == nil {
			ver = string(p)
		}
	}
	st, err := external.LoadSettings(r.Context(), s.Pool, s.Box, prov)
	if err != nil {
		fail("not_configured")
		return
	}
	tok, err := external.ExchangeCode(r.Context(), prov, st.ClientID, st.ClientSecret, redir, code, ver)
	if err != nil {
		fail("token_exchange")
		return
	}
	if err := external.StoreAccount(r.Context(), s.Pool, s.Box, uid, prov, tok); err != nil {
		fail("store")
		return
	}
	if s.Hooks != nil {
		s.Hooks.Emit(r.Context(), "external.provider.connected", map[string]any{"provider": prov, "user_id": uid})
	}
	http.Redirect(w, r, s.absURL(r)+"/settings/connected?connected="+url.QueryEscape(prov), http.StatusFound)
}

func (s *Server) disconnectProvider(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	prov := chi.URLParam(r, "provider")
	keep := r.URL.Query().Get("keep_playlists") != "false"
	var acc uuid.UUID
	_ = s.Pool.QueryRow(r.Context(), `SELECT id FROM external_provider_accounts WHERE user_id=$1 AND provider=$2`, u.ID, prov).Scan(&acc)
	if !keep {
		rows, _ := s.Pool.Query(r.Context(), `SELECT sounddock_playlist_id FROM external_playlists WHERE user_id=$1 AND provider=$2`, u.ID, prov)
		for rows.Next() {
			var pid *uuid.UUID
			_ = rows.Scan(&pid)
			if pid != nil {
				_, _ = s.Pool.Exec(r.Context(), `DELETE FROM playlists WHERE id=$1 AND user_id=$2`, *pid, u.ID)
			}
		}
		rows.Close()
		_, _ = s.Pool.Exec(r.Context(), `DELETE FROM external_playlists WHERE user_id=$1 AND provider=$2`, u.ID, prov)
	}
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM external_provider_accounts WHERE user_id=$1 AND provider=$2`, u.ID, prov)
	if s.Hooks != nil {
		s.Hooks.Emit(r.Context(), "external.provider.disconnected", map[string]any{"provider": prov, "user_id": u.ID})
	}
	_ = acc
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listProviderPlaylists(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	prov := chi.URLParam(r, "provider")
	st, err := external.LoadSettings(r.Context(), s.Pool, s.Box, prov)
	if err != nil || !st.Enabled {
		writeErr(w, 400, "disabled", "provider disabled")
		return
	}
	access, extra, err := s.accountAccess(r, u.ID, prov, st)
	if err != nil {
		writeErr(w, 400, "not_connected", "connect this provider first")
		return
	}
	list, err := external.ListPlaylists(r.Context(), prov, access, extra)
	if err != nil {
		writeErr(w, 502, "provider", err.Error())
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) getProviderPlaylist(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	prov := chi.URLParam(r, "provider")
	id := chi.URLParam(r, "id")
	st, _ := external.LoadSettings(r.Context(), s.Pool, s.Box, prov)
	access, extra, err := s.accountAccess(r, u.ID, prov, st)
	if err != nil && !st.PublicImport {
		writeErr(w, 400, "not_connected", err.Error())
		return
	}
	if access == "" {
		access, extra = publicPair(r, st)
	}
	meta, tracks, err := external.GetPlaylistItems(r.Context(), prov, access, extra, id)
	if err != nil {
		writeErr(w, 502, "provider", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"playlist": meta, "tracks": tracks})
}

func (s *Server) importProviderPlaylist(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !auth.HasPerm(u, "playlists.external_import") {
		writeErr(w, 403, "forbidden", "not permitted")
		return
	}
	prov := chi.URLParam(r, "provider")
	extID := chi.URLParam(r, "id")
	var body struct {
		Mode, Name, Interval, Removal string
	}
	_ = decodeJSON(r, &body)
	if body.Mode == "" {
		body.Mode = "once"
	}
	jid, err := s.Jobs.Enqueue(r.Context(), "external.playlist.import", external.ImportPayload{
		UserID: u.ID, Provider: prov, ExternalID: extID, Mode: body.Mode, Name: body.Name,
		Interval: body.Interval, Removal: body.Removal, LibraryIDs: s.libraryIDs(r.Context(), u),
	})
	if err != nil {
		writeErr(w, 500, "job", err.Error())
		return
	}
	if s.Hooks != nil {
		s.Hooks.Emit(r.Context(), "external.playlist.imported", map[string]any{"provider": prov, "external_id": extID, "job_id": jid})
	}
	writeJSON(w, 202, map[string]any{"job_id": jid})
}

func (s *Server) importPlaylistURL(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !auth.HasPerm(u, "playlists.external_import") {
		writeErr(w, 403, "forbidden", "not permitted")
		return
	}
	var body struct {
		URL, Mode, Name, Interval, Removal string
	}
	_ = decodeJSON(r, &body)
	ref, ok := external.ParsePlaylistURL(body.URL)
	if !ok {
		writeErr(w, 400, "invalid", "not a Spotify, YouTube, SoundCloud, or Apple Music playlist URL")
		return
	}
	if body.Mode == "" {
		body.Mode = "once"
	}
	jid, err := s.Jobs.Enqueue(r.Context(), "external.playlist.import", external.ImportPayload{
		UserID: u.ID, Provider: ref.Provider, ExternalID: ref.ID, Mode: body.Mode, Name: body.Name,
		Interval: body.Interval, Removal: body.Removal, LibraryIDs: s.libraryIDs(r.Context(), u),
	})
	if err != nil {
		writeErr(w, 500, "job", err.Error())
		return
	}
	writeJSON(w, 202, map[string]any{"job_id": jid, "provider": ref.Provider, "external_id": ref.ID})
}

func (s *Server) playlistExternalSync(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	u := currentUser(r)
	var epid uuid.UUID
	var prov, ext, mode, iv, rem string
	err := s.Pool.QueryRow(r.Context(), `SELECT id, provider, external_playlist_id, sync_mode, sync_interval, removal_policy FROM external_playlists WHERE sounddock_playlist_id=$1 AND user_id=$2`, id, u.ID).
		Scan(&epid, &prov, &ext, &mode, &iv, &rem)
	if err != nil {
		writeErr(w, 404, "not_found", "not an external playlist")
		return
	}
	if r.Method == http.MethodGet {
		var last, next *time.Time
		var status, lerr string
		var matched, unmatched int
		_ = s.Pool.QueryRow(r.Context(), `SELECT last_sync_at, next_sync_at, last_sync_status, last_error FROM external_playlists WHERE id=$1`, epid).Scan(&last, &next, &status, &lerr)
		_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE mapped_track_id IS NOT NULL), count(*) FILTER (WHERE mapped_track_id IS NULL AND NOT ignored) FROM external_playlist_items WHERE external_playlist_id=$1`, epid).Scan(&matched, &unmatched)
		writeJSON(w, 200, map[string]any{
			"provider": prov, "external_id": ext, "sync_mode": mode, "sync_interval": iv, "removal_policy": rem,
			"last_sync_at": last, "next_sync_at": next, "status": status, "error": lerr,
			"matched": matched, "unmatched": unmatched, "available_of": unmatched + matched,
		})
		return
	}
	jid, err := s.Jobs.Enqueue(r.Context(), "external.playlist.import", external.ImportPayload{
		UserID: u.ID, Provider: prov, ExternalID: ext, Mode: mode, Interval: iv, Removal: rem,
		PlaylistUUID: id, LibraryIDs: s.libraryIDs(r.Context(), u),
	})
	if err != nil {
		writeErr(w, 500, "job", err.Error())
		return
	}
	writeJSON(w, 202, map[string]any{"job_id": jid})
}

func (s *Server) playlistUnmatched(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	u := currentUser(r)
	rows, err := s.Pool.Query(r.Context(), `
		SELECT i.id, i.position, i.provider_track_id, i.title, i.artists, i.album, i.duration_ms, i.isrc, i.match_status, i.match_confidence, i.source_url
		FROM external_playlist_items i
		JOIN external_playlists p ON p.id=i.external_playlist_id
		WHERE p.sounddock_playlist_id=$1 AND p.user_id=$2 AND i.mapped_track_id IS NULL AND NOT i.ignored
		ORDER BY i.position`, id, u.ID)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "position", "provider_track_id", "title", "artists", "album", "duration_ms", "isrc", "match_status", "match_confidence", "source_url"))
}

func (s *Server) matchExternalItem(w http.ResponseWriter, r *http.Request) {
	pid, _ := uuid.Parse(chi.URLParam(r, "id"))
	iid, _ := uuid.Parse(chi.URLParam(r, "itemID"))
	u := currentUser(r)
	if r.Method == http.MethodDelete {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE external_playlist_items i SET mapped_track_id=NULL, match_status='unmatched' FROM external_playlists p WHERE i.id=$1 AND i.external_playlist_id=p.id AND p.sounddock_playlist_id=$2 AND p.user_id=$3`, iid, pid, u.ID)
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	var body struct {
		TrackID uuid.UUID `json:"track_id"`
	}
	_ = decodeJSON(r, &body)
	if body.TrackID == uuid.Nil {
		writeErr(w, 400, "invalid", "track_id required")
		return
	}
	var prov, extID string
	err := s.Pool.QueryRow(r.Context(), `SELECT p.provider, i.provider_track_id FROM external_playlist_items i JOIN external_playlists p ON p.id=i.external_playlist_id WHERE i.id=$1 AND p.sounddock_playlist_id=$2 AND p.user_id=$3`, iid, pid, u.ID).Scan(&prov, &extID)
	if err != nil {
		writeErr(w, 404, "not_found", "item not found")
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE external_playlist_items SET mapped_track_id=$2, match_status='exact', match_confidence=1 WHERE id=$1`, iid, body.TrackID)
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO external_track_mappings (provider, provider_track_id, sounddock_track_id, mapping_source, confidence, confirmed_by_user_id) VALUES ($1,$2,$3,'manual',1,$4)
		ON CONFLICT (provider, provider_track_id) DO UPDATE SET sounddock_track_id=EXCLUDED.sounddock_track_id, mapping_source='manual', confirmed_by_user_id=EXCLUDED.confirmed_by_user_id`,
		prov, extID, body.TrackID, u.ID)
	var max int
	_ = s.Pool.QueryRow(r.Context(), `SELECT coalesce(max(position),-1) FROM playlist_entries WHERE playlist_id=$1`, pid).Scan(&max)
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO playlist_entries (playlist_id, track_id, position, added_by) SELECT $1,$2,$3,$4 WHERE NOT EXISTS (SELECT 1 FROM playlist_entries WHERE playlist_id=$1 AND track_id=$2)`, pid, body.TrackID, max+1, u.ID)
	if s.Hooks != nil {
		s.Hooks.Emit(r.Context(), "external.track.matched", map[string]any{"playlist_id": pid, "track_id": body.TrackID})
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminExternalProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT provider, enabled, users_may_connect, public_import, client_id, (client_secret_enc IS NOT NULL), (extra_enc IS NOT NULL), default_sync_interval, min_sync_interval FROM external_provider_settings ORDER BY provider`)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var p, cid, defI, minI string
		var en, may, pub, hasSec, hasExtra bool
		_ = rows.Scan(&p, &en, &may, &pub, &cid, &hasSec, &hasExtra, &defI, &minI)
		out = append(out, map[string]any{
			"provider": p, "enabled": en, "users_may_connect": may, "public_import": pub,
			"client_id": cid, "has_client_secret": hasSec, "has_extra": hasExtra,
			"default_sync_interval": defI, "min_sync_interval": minI,
			"callback_url": external.CallbackURL(s.absURL(r), p),
			"capabilities": external.Caps[p],
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) adminPutExternalProvider(w http.ResponseWriter, r *http.Request) {
	prov := chi.URLParam(r, "provider")
	if !external.Known(prov) {
		writeErr(w, 404, "not_found", "unknown provider")
		return
	}
	st, _ := external.LoadSettings(r.Context(), s.Pool, s.Box, prov)
	var body struct {
		Enabled         *bool             `json:"enabled"`
		UsersMayConnect *bool             `json:"users_may_connect"`
		PublicImport    *bool             `json:"public_import"`
		ClientID        *string           `json:"client_id"`
		ClientSecret    *string           `json:"client_secret"`
		DefaultInterval *string           `json:"default_sync_interval"`
		MinInterval     *string           `json:"min_sync_interval"`
		Extra           map[string]string `json:"extra"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if body.Enabled != nil {
		st.Enabled = *body.Enabled
	}
	if body.UsersMayConnect != nil {
		st.UsersMayConnect = *body.UsersMayConnect
	}
	if body.PublicImport != nil {
		st.PublicImport = *body.PublicImport
	}
	if body.ClientID != nil {
		st.ClientID = *body.ClientID
	}
	if body.ClientSecret != nil && *body.ClientSecret != "" {
		st.ClientSecret = *body.ClientSecret
	} else {
		st.ClientSecret = "" // SaveSettings keeps existing when empty
	}
	if body.DefaultInterval != nil {
		st.DefaultInterval = *body.DefaultInterval
	}
	if body.MinInterval != nil {
		st.MinInterval = *body.MinInterval
	}
	if body.Extra != nil {
		if st.Extra == nil {
			st.Extra = map[string]string{}
		}
		for k, v := range body.Extra {
			if v != "" {
				st.Extra[k] = v
			}
		}
	} else {
		st.Extra = nil
	}
	st.Provider = prov
	if err := external.SaveSettings(r.Context(), s.Pool, s.Box, st); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "external.provider.update", prov, r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) accountAccess(r *http.Request, userID uuid.UUID, prov string, st external.Settings) (string, string, error) {
	extra := st.Extra["developer_token"]
	if extra == "" {
		extra = st.Extra["api_key"]
	}
	var accEnc []byte
	err := s.Pool.QueryRow(r.Context(), `SELECT access_token_enc FROM external_provider_accounts WHERE user_id=$1 AND provider=$2 AND status='connected'`, userID, prov).Scan(&accEnc)
	if err != nil {
		return "", extra, err
	}
	access := ""
	if s.Box != nil && len(accEnc) > 0 {
		if p, e := s.Box.Decrypt(accEnc); e == nil {
			access = string(p)
		}
	}
	return access, extra, nil
}

func publicPair(r *http.Request, st external.Settings) (string, string) {
	a, e := external.ClientCredentials(r.Context(), st.Provider, st.ClientID, st.ClientSecret)
	if e != nil {
		extra := st.Extra["developer_token"]
		if extra == "" {
			extra = st.Extra["api_key"]
		}
		return "", extra
	}
	return a, st.Extra["api_key"]
}
