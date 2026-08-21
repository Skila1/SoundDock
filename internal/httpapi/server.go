package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sounddock/sounddock/internal/artwork"
	"github.com/sounddock/sounddock/internal/audit"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/backup"
	"github.com/sounddock/sounddock/internal/config"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/db"
	"github.com/sounddock/sounddock/internal/httpapi/ratelimit"
	"github.com/sounddock/sounddock/internal/ingest"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/search"
	"github.com/sounddock/sounddock/internal/storage"
	"github.com/sounddock/sounddock/internal/transcode"
	"github.com/sounddock/sounddock/internal/version"
	"github.com/sounddock/sounddock/internal/webhooks"
)

type Server struct {
	Cfg      config.Config
	Pool     *pgxpool.Pool
	Auth     *auth.Service
	Jobs     *jobs.Runner
	Search   *search.Engine
	Play     *playback.Engine
	Art      *artwork.Store
	TX       *transcode.Manager
	Ingest   *ingest.Service
	Backup   *backup.Service
	Audit    *audit.Log
	Hooks    *webhooks.Bus
	Box      *cryptox.Box
	Limit    *ratelimit.Limiter
	Slots    *ratelimit.Slots
	Log      *slog.Logger
	Web      fs.FS
	Draining bool
	SignKey  []byte
	Managed  *storage.Local
	OpenAPI  []byte
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.proxyHeaders)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		AllowOriginFunc:  func(r *http.Request, origin string) bool { return true },
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With", "Upload-Offset", "Upload-Length", "Tus-Resumable"},
		ExposedHeaders:   []string{"Upload-Offset", "Location"},
		AllowCredentials: true,
	}))
	r.Use(noStoreAPI)
	r.Use(s.MaintenanceGuard)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	r.Get("/readyz", s.readyz)
	if s.Cfg.MetricsEnabled {
		r.Group(func(r chi.Router) {
			r.Use(s.metricsAuth)
			r.Handle("/metrics", promhttp.Handler())
		})
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/system/info", s.systemInfo)
		r.Get("/openapi.json", s.openapi)
		r.Get("/setup/status", s.setupStatus)
		r.With(s.limit(ratelimit.ClassAuth)).Post("/setup", s.setup)
		r.With(s.limit(ratelimit.ClassAuth)).Post("/auth/login", s.login)
		r.Get("/auth/discord", s.discordLogin)
		r.Get("/auth/discord/callback", s.discordLoginCallback)
		r.Get("/auth/csrf", s.csrf)
		r.Get("/oauth/{provider}/callback", s.oauthCallback)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.logout)
			r.Post("/auth/logout-all", s.logoutAll)
			r.Get("/me", s.me)
			r.Patch("/me", s.patchMe)
			r.Post("/me/password", s.changePassword)
			r.Get("/me/sessions", s.mySessions)
			r.Delete("/me/sessions/{id}", s.deleteSession)
			r.Get("/me/export", s.exportMe)
			r.Get("/me/identities", s.identities)
			r.Post("/me/identities/discord", s.startDiscordLink)
			r.Post("/link/discord", s.confirmDiscordLink)
			r.Get("/me/tokens", s.listTokens)
			r.Post("/me/tokens", s.createToken)
			r.Delete("/me/tokens/{id}", s.revokeToken)
			r.Get("/announcement", s.publicAnnouncement)

			r.With(s.limit(ratelimit.ClassSearch)).Get("/search", s.search)
			r.Get("/tracks", s.listTracks)
			r.Get("/tracks/{id}", s.getTrack)
			r.Patch("/tracks/{id}", s.patchTrack)
			r.Get("/tracks/{id}/metadata", s.getTrackMetadata)
			r.Patch("/tracks/{id}/metadata", s.patchTrackMetadata)
			r.Post("/tracks/bulk/metadata", s.bulkTrackMetadata)
			r.Put("/tracks/{id}/locks", s.putTrackLock)
			r.Get("/tracks/{id}/waveform", s.getTrackWaveform)
			r.Post("/tracks/{id}/artwork", s.postTrackArtwork)
			r.Post("/tracks/bulk", s.bulkTracks)
			r.Get("/tracks/{id}/artwork", s.trackArtwork)
			r.Get("/albums/{id}/artwork", s.albumArtwork)
			r.Get("/artists/{id}/artwork", s.artistArtwork)
			r.Get("/playlists/{id}/artwork", s.playlistArtwork)
			r.Post("/albums/{id}/artwork", s.postAlbumArtwork)
			r.Post("/artists/{id}/artwork", s.postArtistArtwork)
			r.Patch("/albums/{id}/metadata", s.patchAlbumMetadata)
			r.Patch("/artists/{id}/metadata", s.patchArtistMetadata)
			r.Get("/albums", s.listAlbums)
			r.Get("/albums/{id}", s.getAlbum)
			r.Patch("/albums/{id}", s.patchAlbum)
			r.Post("/albums/merge", s.mergeAlbums)
			r.Get("/artists", s.listArtists)
			r.Get("/artists/{id}", s.getArtist)
			r.Post("/artists/merge", s.mergeArtists)
			r.Get("/genres", s.listGenres)
			r.Get("/libraries", s.listLibraries)
			r.Get("/playlists", s.listPlaylists)
			r.Post("/playlists", s.createPlaylist)
			r.Get("/playlists/folders", s.listPlaylistFolders)
			r.Get("/playlists/invite", s.previewPlaylistInvite)
			r.Post("/playlists/invite/accept", s.acceptPlaylistInvite)
			r.Post("/playlists/import.m3u", s.importM3U)
			r.Get("/playlists/{id}", s.getPlaylist)
			r.Put("/playlists/{id}", s.updatePlaylist)
			r.Delete("/playlists/{id}", s.deletePlaylist)
			r.Post("/playlists/{id}/tracks", s.addPlaylistTracks)
			r.Delete("/playlists/{id}/tracks/{entryID}", s.removePlaylistTrack)
			r.Put("/playlists/{id}/tracks/order", s.reorderPlaylist)
			r.Get("/playlists/{id}/export.m3u", s.exportM3U)
			r.Post("/playlists/{id}/invite", s.createPlaylistInvite)
			r.Get("/playlists/{id}/collaborators", s.listPlaylistCollaborators)
			r.Delete("/playlists/{id}/collaborators/{userID}", s.removePlaylistCollaborator)
			r.Get("/playlists/{id}/snapshots", s.listPlaylistSnapshots)
			r.Post("/playlists/{id}/snapshots", s.createPlaylistSnapshot)
			r.Get("/playlists/{id}/snapshots/{sid}", s.getPlaylistSnapshot)
			r.Post("/playlists/{id}/snapshots/{sid}/restore", s.restorePlaylistSnapshot)
			r.Delete("/playlists/{id}/snapshots/{sid}", s.deletePlaylistSnapshot)
			r.Get("/playlists/{id}/smart", s.getSmartPlaylist)
			r.Put("/playlists/{id}/smart", s.putSmartPlaylist)
			r.Post("/playlists/{id}/smart/refresh", s.refreshSmartPlaylist)
			r.Get("/playlists/{id}/sync-diff", s.playlistSyncDiff)
			r.Get("/playlists/{id}/external-sync", s.playlistExternalSync)
			r.Post("/playlists/{id}/external-sync", s.playlistExternalSync)
			r.Get("/playlists/{id}/unmatched", s.playlistUnmatched)
			r.Post("/playlists/{id}/items/{itemID}/match", s.matchExternalItem)
			r.Delete("/playlists/{id}/items/{itemID}/match", s.matchExternalItem)

			r.Get("/radio", s.getRadio)
			r.Get("/radio/seeds", s.radioSeeds)
			r.Post("/radio/refresh", s.radioRefresh)

			r.Get("/me/providers", s.meProviders)
			r.With(s.limit(ratelimit.ClassExternal)).Post("/me/providers/{provider}/connect", s.connectProvider)
			r.Delete("/me/providers/{provider}", s.disconnectProvider)
			r.Get("/providers/{provider}/playlists", s.listProviderPlaylists)
			r.Get("/providers/{provider}/playlists/{id}", s.getProviderPlaylist)
			r.With(s.limit(ratelimit.ClassExternal)).Post("/providers/{provider}/playlists/{id}/import", s.importProviderPlaylist)
			r.With(s.limit(ratelimit.ClassExternal)).Post("/providers/import-url", s.importPlaylistURL)
			r.Post("/favourites", s.setFavourite)
			r.Get("/favourites", s.listFavourites)
			r.Get("/history", s.history)
			r.Get("/home", s.home)
			r.Get("/me/history", s.historyRecent)
			r.Get("/me/never-played", s.neverPlayed)
			r.Get("/me/rediscovery", s.rediscovery)
			r.Get("/me/stats", s.listeningStats)
			r.Get("/me/wrapped", s.wrapped)

			r.Get("/me/queue", s.getQueue)
			r.Put("/me/queue", s.putQueue)
			r.Post("/me/queue/add", s.queueAdd)
			r.Post("/me/queue/control", s.queueControl)
			r.Get("/me/party", s.getParty)
			r.Post("/me/party", s.postParty)
			r.Post("/me/party/votes", s.postPartyVote)
			r.Post("/me/offline/tokens", s.mintOfflineToken)
			r.Delete("/me/offline/tokens", s.revokeOfflineTokens)

			s.MountP7(r)

			r.Post("/stream-tokens", s.streamTokens)
			r.Post("/imports/url", s.importURL)
			r.Get("/imports/jobs", s.importJobs)
			r.Post("/uploads", s.createUpload)
			r.Post("/uploads/finalize", s.finalizeUploads)
			r.Patch("/uploads/{id}", s.patchUpload)
			r.Post("/uploads/{id}/complete", s.completeUpload)

			r.Get("/duplicates", s.duplicates)

			r.Group(func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.With(s.limit(ratelimit.ClassAdmin)).Route("/admin", func(r chi.Router) {
					r.Get("/overview", s.adminOverview)
					r.Get("/health", s.adminHealth)
					r.Get("/health/detail", s.adminHealthDetail)
					r.Get("/announcement", s.adminAnnouncementGet)
					r.Put("/announcement", s.adminAnnouncementPut)
					r.Get("/maintenance", s.adminMaintenanceGet)
					r.Put("/maintenance", s.adminMaintenancePut)
					r.Get("/quotas", s.adminQuotasGet)
					r.Put("/quotas", s.adminQuotasPut)
					r.Get("/stream-policy", s.adminGetStreamPolicy)
					r.Put("/stream-policy", s.adminPutStreamPolicy)
					r.Get("/library/health", s.libraryHealth)
					r.Get("/library/settings", s.libraryIngestSettings)
					r.Put("/library/settings", s.libraryPutIngestSettings)
					r.Get("/library/orphans", s.libraryOrphans)
					r.Get("/library/files-removed", s.libraryFilesRemoved)
					r.Post("/library/integrity/scan", s.libraryIntegrityScan)
					r.Post("/library/files/{id}/trash", s.libraryTrashFile)
					r.Post("/library/files/{id}/restore", s.libraryRestoreFile)
					r.Get("/library/duplicates", s.libraryDuplicateGroups)
					r.Get("/libraries/{id}/grants", s.adminLibraryGrantsGet)
					r.Post("/libraries/{id}/grants", s.adminLibraryGrantAdd)
					r.Delete("/libraries/{id}/grants/{grantID}", s.adminLibraryGrantDelete)
					r.Get("/diagnostics", s.adminDiagnostics)
					r.Get("/demo", s.adminDemoGet)
					r.Post("/demo", s.adminDemoSeed)
					r.Delete("/demo", s.adminDemoUnseed)
					r.Get("/database", s.adminDatabase)
					r.Get("/jobs", s.adminJobs)
					r.Post("/jobs/{id}/cancel", s.cancelJob)
					r.Get("/scans", s.adminScans)
					r.Get("/users", s.adminUsers)
					r.Post("/users", s.adminCreateUser)
					r.Get("/users/{id}", s.adminGetUser)
					r.Patch("/users/{id}", s.adminPatchUser)
					r.Delete("/users/{id}/identities/discord", s.adminUnlinkDiscord)
					r.Delete("/users/{id}", s.adminDeleteUser)
					r.Get("/storage", s.adminStorage)
					r.Post("/storage", s.adminCreateStorage)
					r.Post("/libraries", s.adminCreateLibrary)
					r.Post("/libraries/{id}/scan", s.adminScan)
					r.Post("/libraries/{id}/migrate", s.adminMigrate)
					r.Patch("/libraries/{id}", s.adminPatchLibrary)
					r.Get("/transcode", s.adminTranscode)
					r.Delete("/transcode/cache", s.adminClearCache)
					r.Get("/backups", s.adminBackups)
					r.Post("/backups", s.adminBackup)
					r.Get("/backups/{id}/preview", s.adminBackupPreview)
					r.Post("/backups/{id}/restore", s.adminBackupRestore)
					r.Get("/audit", s.adminAudit)
					r.Get("/retention", s.adminRetention)
					r.Put("/retention", s.adminPutRetention)
					r.Get("/webhooks", s.adminWebhooks)
					r.Post("/webhooks", s.adminCreateWebhook)
					r.Delete("/webhooks/{id}", s.adminDeleteWebhook)
					r.Get("/integrations", s.adminIntegrations)
					r.Post("/integrations", s.adminCreateIntegration)
					r.Delete("/integrations/{id}", s.adminRevokeIntegration)
					r.Get("/integrations/external-providers", s.adminExternalProviders)
					r.Put("/integrations/external-providers/{provider}", s.adminPutExternalProvider)
					r.Get("/metadata", s.adminMetadata)
					r.Put("/metadata", s.adminPutMetadata)
					r.Get("/logs", s.adminLogs)
					r.Get("/integrations/discord", s.discordGet)
					r.Put("/integrations/discord", s.discordPut)
					r.Post("/integrations/discord/test", s.discordTest)
					r.Get("/integrations/discord/invite", s.discordInvite)
					r.Post("/integrations/discord/commands/sync", s.discordSync)
					r.Get("/integrations/discord/status", s.discordStatus)
					r.Get("/integrations/discord/guilds", s.discordGuilds)
					r.Patch("/integrations/discord/guilds/{id}", s.discordPatchGuild)
					r.Post("/integrations/discord/guilds/{id}/disconnect", s.discordDisconnect)
					r.Get("/integrations/discord/sessions", s.discordSessions)
					r.Get("/integrations/discord/logs", s.discordLogs)
					r.Get("/updates", s.adminUpdatesGet)
					r.Put("/updates", s.adminUpdatesPut)
					r.Post("/updates/check", s.adminUpdatesCheck)
					r.Post("/updates/apply", s.adminUpdatesApply)
				})
			})
		})
	})

	r.Get("/api/v1/tracks/{id}/stream", s.streamTrack) // cookie or token; not behind requireAuth header-only
	r.Get("/rest/*", s.openSubsonic)
	r.Get("/api/docs", s.docs)

	if s.Web != nil {
		r.NotFound(s.spa().ServeHTTP)
	}
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"code": code, "message": msg})
}

func (s *Server) proxyHeaders(next http.Handler) http.Handler {
	nets := s.Cfg.TrustedNets()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := net.ParseIP(strings.Split(r.RemoteAddr, ":")[0])
		trusted := false
		for _, n := range nets {
			if ip != nil && n.Contains(ip) {
				trusted = true
				break
			}
		}
		if trusted {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				r.RemoteAddr = strings.TrimSpace(strings.Split(xff, ",")[0])
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) limit(c ratelimit.Class) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.RemoteAddr
			if !s.Limit.Allow(c, key) {
				writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) metricsAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Cfg.MetricsToken != "" && r.Header.Get("Authorization") != "Bearer "+s.Cfg.MetricsToken {
			writeErr(w, 401, "unauthorized", "metrics token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.Draining {
		writeErr(w, 503, "draining", "instance draining")
		return
	}
	if err := s.Pool.Ping(r.Context()); err != nil {
		writeErr(w, 503, "not_ready", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":     true,
		"ffmpeg": transcode.FFmpegAvailable(),
	})
}

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	oauth := auth.LoadDiscordOAuth(r.Context(), s.Pool, s.Box)
	writeJSON(w, 200, map[string]any{
		"name":             s.Cfg.InstanceName,
		"version":          version.Version,
		"api_version":      version.APIVersion,
		"codecs":           []string{"mp3", "flac", "aac", "m4a", "alac", "ogg", "opus", "wav"},
		"opensubsonic":     s.Cfg.OpenSubsonic,
		"discord_optional": true,
		"discord_auth":     oauth.Ready(),
		"features": map[string]bool{
			"search": true, "playlists": true, "uploads": true, "remote_import": true,
			"external_playlists": true, "webhooks": true, "pwa": true, "replaygain": true, "crossfade": true,
			"discord_login": oauth.Ready(),
		},
	})
}

func noStoreAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/rest/") {
			w.Header().Set("Cache-Control", "private, no-store, no-cache, must-revalidate")
			w.Header().Set("CDN-Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) spa() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/rest/") {
			writeErr(w, http.StatusNotFound, "not_found", "unknown api route")
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		f, err := s.Web.Open(p)
		if err != nil {
			http.ServeFileFS(w, r, s.Web, "index.html")
			return
		}
		f.Close()
		http.ServeFileFS(w, r, s.Web, p)
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: "sd_session", Value: token, Path: "/", HttpOnly: true,
		Secure: s.cookieSecureFor(r), SameSite: http.SameSiteLaxMode, Expires: exp,
	})
}

type ctxKey int

const userKey ctxKey = 1

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		if tok == "" {
			if c, err := r.Cookie("sd_session"); err == nil {
				tok = c.Value
			}
		}
		if tok == "" {
			writeErr(w, 401, "unauthorized", "authentication required")
			return
		}
		// API key
		if strings.HasPrefix(tok, "sd_") {
			u, err := s.apiKeyUser(r.Context(), tok)
			if err != nil {
				writeErr(w, 401, "unauthorized", "invalid token")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
			return
		}
		u, _, err := s.Auth.SessionUser(r.Context(), tok)
		if err != nil {
			writeErr(w, 401, "unauthorized", "invalid session")
			return
		}
		if u.Disabled {
			writeErr(w, 403, "disabled", "account disabled")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return r.URL.Query().Get("access_token")
}

func currentUser(r *http.Request) *auth.User {
	u, _ := r.Context().Value(userKey).(*auth.User)
	return u
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil || !u.IsAdmin {
			writeErr(w, 403, "forbidden", "administrator required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) apiKeyUser(ctx context.Context, tok string) (*auth.User, error) {
	hash := cryptox.HashToken(tok)
	var uid uuid.UUID
	err := s.Pool.QueryRow(ctx, `SELECT user_id FROM personal_access_tokens WHERE secret_hash=$1 AND revoked_at IS NULL`, hash).Scan(&uid)
	if err == nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE personal_access_tokens SET last_used_at=now() WHERE secret_hash=$1 AND revoked_at IS NULL`, hash)
		return s.Auth.GetUser(ctx, uid)
	}
	var cid uuid.UUID
	err = s.Pool.QueryRow(ctx, `SELECT client_id FROM api_client_keys WHERE secret_hash=$1 AND revoked_at IS NULL`, hash).Scan(&cid)
	if err != nil {
		return nil, err
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE api_clients SET last_used_at=now() WHERE id=$1`, cid)
	u := &auth.User{ID: cid, Username: "integration", Permissions: []string{"tracks.read", "tracks.stream", "library.read", "playlists.write", "history.read"}}
	var scopes []string
	_ = s.Pool.QueryRow(ctx, `SELECT scopes FROM api_clients WHERE id=$1`, cid).Scan(&scopes)
	if len(scopes) > 0 {
		u.Permissions = scopes
		for _, sc := range scopes {
			if sc == "admin" {
				u.IsAdmin = true
			}
		}
	}
	return u, nil
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(v)
}

func idsOrEmpty(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}

func (s *Server) libraryIDs(ctx context.Context, u *auth.User) []uuid.UUID {
	if u.IsAdmin {
		rows, err := s.Pool.Query(ctx, `SELECT id FROM libraries`)
		if err != nil {
			return []uuid.UUID{}
		}
		defer rows.Close()
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		return idsOrEmpty(ids)
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT library_id FROM library_grants WHERE user_id=$1
		UNION
		SELECT lg.library_id FROM library_grants lg JOIN user_roles ur ON ur.role_id=lg.role_id WHERE ur.user_id=$1`, u.ID)
	if err != nil {
		return []uuid.UUID{}
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	return idsOrEmpty(ids)
}

func (s *Server) ProviderFor(ctx context.Context, lib uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error) {
	var sid uuid.UUID
	var typ, prefix, name string
	var cfg []byte
	var ro bool
	err := s.Pool.QueryRow(ctx, `
		SELECT l.storage_provider_id, l.root_prefix, l.read_only, sp.type, sp.config_enc, sp.name
		FROM libraries l JOIN storage_providers sp ON sp.id=l.storage_provider_id WHERE l.id=$1`, lib).
		Scan(&sid, &prefix, &ro, &typ, &cfg, &name)
	if err != nil {
		return nil, uuid.Nil, "", err
	}
	plain := cfg
	if s.Box != nil && len(cfg) > 0 {
		if p, err := s.Box.Decrypt(cfg); err == nil {
			plain = p
		}
	}
	switch typ {
	case "local", "managed":
		root := string(plain)
		if root == "" {
			root = s.Cfg.ManagedDir
		}
		l, err := storage.NewLocal(sid.String(), root, ro)
		return l, lib, prefix, err
	case "s3":
		var sc storage.S3Config
		_ = json.Unmarshal(plain, &sc)
		p, err := storage.NewS3(sid.String(), sc)
		return p, lib, prefix, err
	default:
		return nil, uuid.Nil, "", errors.New("unknown storage")
	}
}

func (s *Server) docs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(`<!doctype html><html><head><title>SoundDock API</title>
<script id="api-reference" data-url="/api/v1/openapi.json"></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script></head><body></body></html>`))
}

func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if len(s.OpenAPI) > 0 {
		w.Write(s.OpenAPI)
		return
	}
	writeJSON(w, 200, map[string]any{"openapi": "3.0.3", "info": map[string]string{"title": "SoundDock API", "version": version.APIVersion}})
}

func (s *Server) csrf(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"csrf": "cookie"})
}

func absFile(p string) string { return filepath.Clean(p) }

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

var errNoRows = pgx.ErrNoRows

func (s *Server) dbVersion(ctx context.Context) (int64, bool) {
	v, d, _ := db.Version(ctx, s.Pool)
	return v, d
}
