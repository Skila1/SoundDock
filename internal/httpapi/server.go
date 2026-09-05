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
	"sync"
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
	"github.com/sounddock/sounddock/internal/mediabusy"
	"github.com/sounddock/sounddock/internal/oplog"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/retention"
	"github.com/sounddock/sounddock/internal/scapex"
	"github.com/sounddock/sounddock/internal/search"
	"github.com/sounddock/sounddock/internal/storage"
	"github.com/sounddock/sounddock/internal/transcode"
	"github.com/sounddock/sounddock/internal/version"
	"github.com/sounddock/sounddock/internal/webhooks"
)

type Server struct {
	Cfg       config.Config
	Pool      *pgxpool.Pool
	Auth      *auth.Service
	Jobs      *jobs.Runner
	Search    *search.Engine
	Play      *playback.Engine
	Art       *artwork.Store
	TX        *transcode.Manager
	Ingest    *ingest.Service
	Backup    *backup.Service
	Audit     *audit.Log
	Hooks     *webhooks.Bus
	Box       *cryptox.Box
	Limit     *ratelimit.Limiter
	Slots     *ratelimit.Slots
	MediaBusy *mediabusy.Set
	Log       *slog.Logger
	Web       fs.FS
	Draining  bool
	SignKey   []byte
	Managed   *storage.Local
	OpenAPI   []byte
	ScapeX    *scapex.Client
	Retention *retention.Engine

	hubOnce sync.Once
	hub     *sessionHub

	// youtubeFillHook replaces similarYouTube in tests. Production stays nil.
	youtubeFillHook func(ctx context.Context, seed uuid.UUID, need int, have []uuid.UUID) []string
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
		r.Group(func(r chi.Router) {
			r.Use(s.requireSetupNeeded)
			r.Get("/setup/backups/settings", s.adminBackupSettingsGet)
			r.Put("/setup/backups/settings", s.adminBackupSettingsPut)
			r.Get("/setup/backups/remote", s.adminBackupRemote)
			r.Post("/setup/backups/import-remote", s.adminBackupImportRemote)
		})
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
			r.Get("/me/library", s.requirePerm("tracks.read", s.myPersonalLibrary))
			r.Get("/users/{id}", s.requirePerm("tracks.read", s.userPublicProfile))
			r.Get("/users/{id}/library", s.requirePerm("tracks.read", s.userPersonalLibrary))
			r.Post("/me/password", s.changePassword)
			r.Get("/me/sessions", s.mySessions)
			r.Delete("/me/sessions/{id}", s.deleteSession)
			r.Get("/me/export", s.exportMe)
			r.Get("/me/identities", s.identities)
			r.Get("/me/tokens", s.listTokens)
			r.Post("/me/tokens", s.createToken)
			r.Delete("/me/tokens/{id}", s.revokeToken)
			r.Get("/announcement", s.publicAnnouncement)

			r.With(s.limit(ratelimit.ClassSearch)).Get("/search/youtube", s.requirePerm("tracks.read", s.searchYouTube))
			r.With(s.limit(ratelimit.ClassSearch)).Get("/search", s.requirePerm("tracks.read", s.search))
			r.Get("/tracks", s.requirePerm("tracks.read", s.listTracks))
			r.Get("/tracks/{id}", s.requirePerm("tracks.read", s.getTrack))
			r.Get("/tracks/{id}/playability", s.requirePerm("tracks.read", s.getTrackPlayability))
			r.Get("/tracks/{id}/lyrics", s.requirePerm("tracks.read", s.getTrackLyrics))
			r.Patch("/tracks/{id}", s.patchTrack)
			r.Get("/tracks/{id}/metadata", s.requirePerm("tracks.read", s.getTrackMetadata))
			r.Patch("/tracks/{id}/metadata", s.patchTrackMetadata)
			r.Post("/tracks/bulk/metadata", s.bulkTrackMetadata)
			r.Put("/tracks/{id}/locks", s.putTrackLock)
			r.Get("/tracks/{id}/waveform", s.requirePerm("tracks.read", s.getTrackWaveform))
			r.Post("/tracks/{id}/artwork", s.postTrackArtwork)
			r.Post("/tracks/bulk", s.bulkTracks)
			r.Delete("/tracks/bulk", s.bulkTracks)
			r.Get("/tracks/{id}/artwork", s.requirePerm("tracks.read", s.trackArtwork))
			r.Get("/albums/{id}/artwork", s.requirePerm("tracks.read", s.albumArtwork))
			r.Get("/artists/{id}/artwork", s.requirePerm("tracks.read", s.artistArtwork))
			r.Get("/playlists/{id}/artwork", s.requirePerm("tracks.read", s.playlistArtwork))
			r.Post("/albums/{id}/artwork", s.postAlbumArtwork)
			r.Post("/artists/{id}/artwork", s.postArtistArtwork)
			r.Patch("/albums/{id}/metadata", s.patchAlbumMetadata)
			r.Patch("/artists/{id}/metadata", s.patchArtistMetadata)
			r.Get("/albums", s.requirePerm("tracks.read", s.listAlbums))
			r.Get("/albums/{id}", s.requirePerm("tracks.read", s.getAlbum))
			r.Patch("/albums/{id}", s.patchAlbum)
			r.Post("/albums/merge", s.mergeAlbums)
			r.Get("/artists", s.requirePerm("tracks.read", s.listArtists))
			r.Get("/artists/{id}", s.requirePerm("tracks.read", s.getArtist))
			r.Post("/artists/merge", s.mergeArtists)
			r.Get("/genres", s.requirePerm("tracks.read", s.listGenres))
			r.Get("/libraries", s.requirePerm("tracks.read", s.listLibraries))
			r.Get("/playlists", s.requirePerm("tracks.read", s.listPlaylists))
			r.Post("/playlists", s.requirePerm("playlists.write", s.createPlaylist))
			r.Get("/playlists/folders", s.requirePerm("tracks.read", s.listPlaylistFolders))
			r.Get("/playlists/invite", s.previewPlaylistInvite)
			r.Post("/playlists/invite/accept", s.acceptPlaylistInvite)
			r.Post("/playlists/import.m3u", s.requirePerm("playlists.write", s.importM3U))
			r.Get("/playlists/{id}", s.requirePerm("tracks.read", s.getPlaylist))
			r.Put("/playlists/{id}", s.requirePerm("playlists.write", s.updatePlaylist))
			r.Delete("/playlists/{id}", s.requirePerm("playlists.write", s.deletePlaylist))
			r.Post("/playlists/{id}/tracks", s.requirePerm("playlists.write", s.addPlaylistTracks))
			r.Delete("/playlists/{id}/tracks/{entryID}", s.requirePerm("playlists.write", s.removePlaylistTrack))
			r.Put("/playlists/{id}/tracks/order", s.requirePerm("playlists.write", s.reorderPlaylist))
			r.Get("/playlists/{id}/export.m3u", s.requirePerm("tracks.read", s.exportM3U))
			r.Post("/playlists/{id}/invite", s.requirePerm("playlists.write", s.createPlaylistInvite))
			r.Get("/playlists/{id}/collaborators", s.requirePerm("tracks.read", s.listPlaylistCollaborators))
			r.Delete("/playlists/{id}/collaborators/{userID}", s.requirePerm("playlists.write", s.removePlaylistCollaborator))
			r.Get("/playlists/{id}/snapshots", s.requirePerm("tracks.read", s.listPlaylistSnapshots))
			r.Post("/playlists/{id}/snapshots", s.requirePerm("playlists.write", s.createPlaylistSnapshot))
			r.Get("/playlists/{id}/snapshots/{sid}", s.requirePerm("tracks.read", s.getPlaylistSnapshot))
			r.Post("/playlists/{id}/snapshots/{sid}/restore", s.requirePerm("playlists.write", s.restorePlaylistSnapshot))
			r.Delete("/playlists/{id}/snapshots/{sid}", s.requirePerm("playlists.write", s.deletePlaylistSnapshot))
			r.Get("/playlists/{id}/smart", s.requirePerm("tracks.read", s.getSmartPlaylist))
			r.Put("/playlists/{id}/smart", s.requirePerm("playlists.write", s.putSmartPlaylist))
			r.Post("/playlists/{id}/smart/refresh", s.requirePerm("playlists.write", s.refreshSmartPlaylist))
			r.Get("/playlists/{id}/sync-diff", s.requirePerm("tracks.read", s.playlistSyncDiff))
			r.Get("/playlists/{id}/external-sync", s.requirePerm("tracks.read", s.playlistExternalSync))
			r.Post("/playlists/{id}/external-sync", s.requirePerm("playlists.write", s.playlistExternalSync))
			r.Get("/playlists/{id}/unmatched", s.requirePerm("tracks.read", s.playlistUnmatched))
			r.Post("/playlists/{id}/items/{itemID}/match", s.requirePerm("playlists.write", s.matchExternalItem))
			r.Delete("/playlists/{id}/items/{itemID}/match", s.requirePerm("playlists.write", s.matchExternalItem))
			r.Post("/playlists/{id}/items/{itemID}/youtube", s.youtubeFillExternalItem)

			r.Get("/radio", s.requirePerm("tracks.read", s.getRadio))
			r.Get("/radio/seeds", s.requirePerm("tracks.read", s.radioSeeds))
			r.Post("/radio/refresh", s.requirePerm("tracks.read", s.radioRefresh))

			r.Get("/me/providers", s.meProviders)
			r.With(s.limit(ratelimit.ClassExternal)).Post("/me/providers/{provider}/connect", s.connectProvider)
			r.Delete("/me/providers/{provider}", s.disconnectProvider)
			r.Get("/providers/{provider}/playlists", s.listProviderPlaylists)
			r.Get("/providers/{provider}/playlists/{id}", s.getProviderPlaylist)
			r.With(s.limit(ratelimit.ClassExternal)).Post("/providers/{provider}/playlists/{id}/import", s.importProviderPlaylist)
			r.With(s.limit(ratelimit.ClassExternal)).Post("/providers/{provider}/import-all", s.importAllProviderPlaylists)
			r.With(s.limit(ratelimit.ClassExternal)).Post("/providers/import-url", s.importPlaylistURL)
			r.Post("/favourites", s.setFavourite)
			r.Get("/favourites", s.requirePerm("tracks.read", s.listFavourites))
			r.Get("/history", s.requirePerm("history.read", s.history))
			r.Get("/home", s.requirePerm("tracks.read", s.home))
			r.Get("/me/history", s.requirePerm("history.read", s.historyRecent))
			r.Get("/me/never-played", s.requirePerm("history.read", s.neverPlayed))
			r.Get("/me/rediscovery", s.requirePerm("history.read", s.rediscovery))
			r.Get("/me/stats", s.requirePerm("history.read", s.listeningStats))
			r.Get("/me/wrapped", s.requirePerm("history.read", s.wrapped))

			r.Get("/me/queue", s.getQueue)
			r.Put("/me/queue", s.putQueue)
			r.Post("/me/queue/add", s.queueAdd)
			r.Post("/me/queue/control", s.queueControl)
			r.Post("/me/queue/renderer/acquire", s.queueRendererAcquire)
			r.Post("/me/queue/heartbeat", s.queueHeartbeat)
			r.With(s.rejectSSEQueryAuth).Get("/me/queue/sse", s.queueSSE)
			r.With(s.rejectSSEQueryAuth).Get("/me/queue/events", s.queueSSE)
			r.Get("/me/party", s.getParty)
			r.Post("/me/party", s.postParty)
			r.Post("/me/party/votes", s.postPartyVote)
			r.Post("/me/offline/tokens", s.mintOfflineToken)
			r.Delete("/me/offline/tokens", s.revokeOfflineTokens)

			s.MountP7(r)

			r.Group(func(r chi.Router) {
				r.Use(s.requirePermMW("tracks.replace_source"))
				s.MountReplaceSource(r)
			})

			r.Post("/stream-tokens", s.requirePerm("tracks.stream", s.streamTokens))
			r.Post("/imports/url", s.importURL)
			r.Get("/imports/jobs", s.importJobs)
			r.Post("/uploads", s.createUpload)
			r.Post("/uploads/finalize", s.finalizeUploads)
			r.Patch("/uploads/{id}", s.patchUpload)
			r.Post("/uploads/{id}/complete", s.completeUpload)

			r.With(s.requireAdmin).Get("/duplicates", s.duplicates)

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
					r.Get("/acquisition-policy", s.adminGetAcquisitionPolicy)
					r.Put("/acquisition-policy", s.adminPutAcquisitionPolicy)
					r.Get("/lyrics", s.requirePerm("lyrics.configure", s.adminGetLyrics))
					r.Put("/lyrics", s.requirePerm("lyrics.configure", s.adminPutLyrics))
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
					r.Patch("/libraries/{id}/grants/{grantID}", s.adminLibraryGrantPatch)
					r.Delete("/libraries/{id}/grants/{grantID}", s.adminLibraryGrantDelete)
					r.Get("/library-grants-strict", s.adminLibraryGrantsStrictGet)
					r.Put("/library-grants-strict", s.adminLibraryGrantsStrictPut)
					r.Get("/diagnostics", s.adminDiagnostics)
					r.Get("/demo", s.adminDemoGet)
					r.Post("/demo", s.adminDemoSeed)
					r.Delete("/demo", s.adminDemoUnseed)
					r.Get("/database", s.adminDatabase)
					r.Get("/jobs", s.adminJobs)
					r.Post("/jobs/{id}/cancel", s.cancelJob)
					r.Post("/jobs/{id}/retry", s.retryJob)
					r.Get("/workers", s.adminWorkersGet)
					r.Put("/workers", s.adminWorkersPut)
					r.Get("/scans", s.adminScans)
					r.Get("/users", s.adminUsers)
					r.Post("/users", s.adminCreateUser)
					r.Get("/users/{id}", s.adminGetUser)
					r.Get("/users/{id}/library", s.adminUserPersonalLibrary)
					r.Get("/discord-users/{discordID}/library", s.adminDiscordPersonalLibrary)
					r.Patch("/users/{id}", s.adminPatchUser)
					r.Delete("/users/{id}/identities/discord", s.adminUnlinkDiscord)
					r.Delete("/users/{id}", s.adminDeleteUser)
					r.Get("/storage", s.adminStorage)
					r.Post("/storage", s.adminCreateStorage)
					r.Patch("/storage/{id}", s.adminPatchStorage)
					r.Delete("/storage/{id}", s.adminDeleteStorage)
					r.Post("/libraries", s.adminCreateLibrary)
					r.Post("/libraries/{id}/scan", s.adminScan)
					r.Post("/libraries/{id}/migrate", s.adminMigrate)
					r.Post("/libraries/{id}/merge", s.requirePerm("tracks.merge", s.adminMergeLibraries))
					r.Group(func(r chi.Router) {
						r.Use(s.requirePermMW("tracks.merge"))
						s.MountDuplicateReview(r)
					})
					r.Post("/libraries/{id}/default", s.adminSetDefaultLibrary)
					r.Patch("/libraries/{id}", s.adminPatchLibrary)
					r.Delete("/libraries/{id}", s.adminDeleteLibrary)
					r.Get("/permissions", s.adminPermissions)
					r.Get("/roles", s.adminRoles)
					r.Post("/roles", s.adminCreateRole)
					r.Patch("/roles/{id}", s.adminPatchRole)
					r.Delete("/roles/{id}", s.adminDeleteRole)
					r.Post("/roles/{id}/members", s.adminRoleMembers)
					r.Delete("/roles/{id}/members", s.adminRoleMembers)
					r.Put("/roles/{id}/discord", s.adminRoleDiscord)
					r.Post("/roles/sync-discord", s.adminSyncDiscordRoles)
					r.Get("/transcode", s.adminTranscode)
					r.Delete("/transcode/cache", s.adminClearCache)
					r.Get("/backups/settings", s.adminBackupSettingsGet)
					r.Put("/backups/settings", s.adminBackupSettingsPut)
					r.Post("/backups/passphrase", s.adminBackupPassphrase)
					r.Get("/backups/reminder", s.adminBackupReminderGet)
					r.Post("/backups/reminder/dismiss", s.adminBackupReminderDismiss)
					r.Get("/backups/restore-requirements", s.adminBackupRequirements)
					r.Post("/backups/restore-requirements/dismiss", s.adminBackupRequirementsDismiss)
					r.Get("/backups/remote", s.adminBackupRemote)
					r.Post("/backups/import-remote", s.adminBackupImportRemote)
					r.Get("/backups", s.adminBackups)
					r.Post("/backups", s.adminBackup)
					r.Get("/backups/{id}/preview", s.adminBackupPreview)
					r.Post("/backups/{id}/restore", s.adminBackupRestore)
					r.Get("/audit", s.adminAudit)
					r.Get("/retention", s.adminRetention)
					r.Put("/retention", s.adminPutRetention)
					r.Post("/retention/preview", s.adminRetentionPreview)
					r.Post("/retention/run", s.adminRetentionRun)
					r.Post("/retention/exclusions", s.adminRetentionExclusion)
					r.Delete("/retention/exclusions/{id}", s.adminDeleteRetentionExclusion)
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
					r.Post("/metadata/refresh", s.adminRefreshMetadata)
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
					r.Get("/listen-compare", s.adminListenCompare)
					r.Get("/stats/rebuild", s.adminStatsRebuildGet)
					r.Post("/stats/rebuild", s.adminStatsRebuildPost)
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
	if status >= 500 {
		writeInternal(w, status, code, msg)
		return
	}
	writeJSON(w, status, map[string]any{"code": code, "message": msg})
}

func writeInternal(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"code": code, "message": sanitizeInternal(msg)})
}

func sanitizeInternal(msg string) string {
	s := oplog.Redact(msg)
	low := strings.ToLower(s)
	for _, needle := range []string{"pq:", "sqlstate", "password", "secret", "master_key", "postgres://"} {
		if strings.Contains(low, needle) {
			return "An internal error occurred"
		}
	}
	return s
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
		"name":                  s.Cfg.InstanceName,
		"version":               version.Version,
		"api_version":           version.APIVersion,
		"codecs":                []string{"mp3", "flac", "aac", "m4a", "alac", "ogg", "opus", "wav"},
		"opensubsonic":          s.Cfg.OpenSubsonic,
		"discord_optional":      true,
		"discord_auth":          oauth.Ready(),
		"library_grants_strict": s.libraryGrantsStrict(r.Context()),
		"features": map[string]bool{
			"search": true, "playlists": true, "uploads": true, "remote_import": true,
			"external_playlists": true, "webhooks": true, "pwa": true, "replaygain": true, "crossfade": true,
			"discord_login": oauth.Ready(), "scapex": s.ScapeX != nil && s.ScapeX.Ready(r.Context()),
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
		// Personal access tokens are sdp_; integration keys are sd_.
		if isAPIToken(tok) {
			u, err := s.apiKeyUser(r.Context(), tok)
			if err != nil {
				writeErr(w, 401, "unauthorized", "invalid token")
				return
			}
			if rejectIfDisabled(w, u) {
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

func rejectIfDisabled(w http.ResponseWriter, u *auth.User) bool {
	if u != nil && u.Disabled {
		writeErr(w, 403, "disabled", "account disabled")
		return true
	}
	return false
}

func isAPIToken(tok string) bool {
	return strings.HasPrefix(tok, patTokenPrefix) || strings.HasPrefix(tok, "sd_")
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

const settingLibraryGrantsStrict = "library_grants_strict"

// Catalogue actions on library_grants.actions. "admin" on a row is not a
// catalogue action; unknown-only rows follow empty-actions compatibility.
var knownLibraryGrantActions = map[string]struct{}{
	"read": {}, "stream": {}, "write": {},
}

func (s *Server) libraryGrantsStrict(ctx context.Context) bool {
	if s == nil || s.Pool == nil {
		return false
	}
	var on bool
	s.settingJSON(ctx, settingLibraryGrantsStrict, &on)
	return on
}

func (s *Server) adminLibraryGrantsStrictGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"library_grants_strict": s.libraryGrantsStrict(r.Context())})
}

func (s *Server) adminLibraryGrantsStrictPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LibraryGrantsStrict *bool `json:"library_grants_strict"`
	}
	if err := decodeJSON(r, &body); err != nil || body.LibraryGrantsStrict == nil {
		writeErr(w, 400, "invalid", "library_grants_strict required")
		return
	}
	if err := s.putSetting(r.Context(), settingLibraryGrantsStrict, *body.LibraryGrantsStrict); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if s.Audit != nil {
		s.Audit.Event(r.Context(), &currentUser(r).ID, "library_grants_strict.update", "", r.RemoteAddr, map[string]any{
			"library_grants_strict": *body.LibraryGrantsStrict,
		})
	}
	writeJSON(w, 200, map[string]bool{"library_grants_strict": *body.LibraryGrantsStrict})
}

// grantActionsAllow reports whether a grant row covers want.
// Compatibility (strict=false): empty or unknown actions grant read+stream
// (current visibility). strict=true requires the action to be listed.
func grantActionsAllow(actions []string, want string, strict bool) bool {
	if want == "" {
		return false
	}
	hasKnown := false
	for _, a := range actions {
		if _, ok := knownLibraryGrantActions[a]; ok {
			hasKnown = true
		}
		if a == want {
			return true
		}
	}
	if strict {
		return false
	}
	if !hasKnown && (want == "read" || want == "stream") {
		return true
	}
	return false
}

func (s *Server) libraryIDs(ctx context.Context, u *auth.User) []uuid.UUID {
	return s.libraryIDsFor(ctx, u, "read")
}

func (s *Server) libraryIDsFor(ctx context.Context, u *auth.User, action string) []uuid.UUID {
	if u == nil || s.Pool == nil {
		return []uuid.UUID{}
	}
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
		SELECT library_id, actions FROM library_grants WHERE user_id=$1
		UNION ALL
		SELECT lg.library_id, lg.actions FROM library_grants lg
		JOIN user_roles ur ON ur.role_id=lg.role_id WHERE ur.user_id=$1`, u.ID)
	if err != nil {
		return []uuid.UUID{}
	}
	defer rows.Close()
	strict := s.libraryGrantsStrict(ctx)
	allowed := map[uuid.UUID]struct{}{}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var actions []string
		if err := rows.Scan(&id, &actions); err != nil {
			continue
		}
		if _, ok := allowed[id]; ok {
			continue
		}
		if grantActionsAllow(actions, action, strict) {
			allowed[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return idsOrEmpty(ids)
}

func (s *Server) userHasLibraryAction(ctx context.Context, u *auth.User, libID uuid.UUID, action string) bool {
	if u == nil || libID == uuid.Nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	for _, id := range s.libraryIDsFor(ctx, u, action) {
		if id == libID {
			return true
		}
	}
	return false
}

func (s *Server) requireLibraryWrite(w http.ResponseWriter, r *http.Request, libID uuid.UUID) bool {
	return s.requireLibraryAction(w, r, libID, "write", "not found")
}

// denyLibraryAction writes 404 for catalogue read (no existence leak) and
// 403 library_grant for stream/write of a hidden resource.
func denyLibraryAction(w http.ResponseWriter, action, notFoundMsg string) {
	if action == "read" {
		writeErr(w, http.StatusNotFound, "not_found", notFoundMsg)
		return
	}
	msg := "library write not granted"
	if action == "stream" {
		msg = "library stream not granted"
	}
	writeErr(w, http.StatusForbidden, "library_grant", msg)
}

func (s *Server) requireLibraryAction(w http.ResponseWriter, r *http.Request, libID uuid.UUID, action, notFoundMsg string) bool {
	u := currentUser(r)
	if u != nil && u.IsAdmin {
		return true
	}
	if libID == uuid.Nil || !s.userHasLibraryAction(r.Context(), u, libID, action) {
		denyLibraryAction(w, action, notFoundMsg)
		return false
	}
	return true
}

func (s *Server) requireTrackLibraryWrite(w http.ResponseWriter, r *http.Request, trackID uuid.UUID) bool {
	return s.requireTrackLibrary(w, r, trackID, "write")
}

func (s *Server) requireTrackLibrary(w http.ResponseWriter, r *http.Request, trackID uuid.UUID, action string) bool {
	if trackID == uuid.Nil {
		writeErr(w, 400, "invalid", "track id")
		return false
	}
	if s.Pool == nil {
		if action == "read" {
			writeErr(w, http.StatusNotFound, "not_found", "track not found")
			return false
		}
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "database unavailable")
		return false
	}
	var libID uuid.UUID
	if err := s.Pool.QueryRow(r.Context(), `SELECT library_id FROM tracks WHERE id=$1`, trackID).Scan(&libID); err != nil {
		writeErr(w, 404, "not_found", "track not found")
		return false
	}
	return s.requireLibraryAction(w, r, libID, action, "track not found")
}

func (s *Server) requireAlbumLibrary(w http.ResponseWriter, r *http.Request, albumID uuid.UUID, action string) bool {
	if albumID == uuid.Nil {
		writeErr(w, 400, "invalid", "album id")
		return false
	}
	if s.Pool == nil {
		if action == "read" {
			writeErr(w, http.StatusNotFound, "not_found", "album not found")
			return false
		}
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "database unavailable")
		return false
	}
	var libID *uuid.UUID
	if err := s.Pool.QueryRow(r.Context(), `SELECT library_id FROM albums WHERE id=$1`, albumID).Scan(&libID); err != nil {
		writeErr(w, 404, "not_found", "album not found")
		return false
	}
	id := uuid.Nil
	if libID != nil {
		id = *libID
	}
	return s.requireLibraryAction(w, r, id, action, "album not found")
}

func (s *Server) requireArtistLibrary(w http.ResponseWriter, r *http.Request, artistID uuid.UUID, action string) bool {
	if artistID == uuid.Nil {
		writeErr(w, 400, "invalid", "artist id")
		return false
	}
	if s.Pool == nil {
		if action == "read" {
			writeErr(w, http.StatusNotFound, "not_found", "artist not found")
			return false
		}
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "database unavailable")
		return false
	}
	var name string
	if err := s.Pool.QueryRow(r.Context(), `SELECT name FROM artists WHERE id=$1`, artistID).Scan(&name); err != nil {
		writeErr(w, 404, "not_found", "artist not found")
		return false
	}
	u := currentUser(r)
	if u != nil && u.IsAdmin {
		return true
	}
	libs := s.artistLibraryIDs(r.Context(), artistID)
	if action == "read" {
		for _, id := range libs {
			if s.userHasLibraryAction(r.Context(), u, id, "read") {
				return true
			}
		}
		writeErr(w, 404, "not_found", "artist not found")
		return false
	}
	if len(libs) == 0 {
		denyLibraryAction(w, action, "artist not found")
		return false
	}
	for _, id := range libs {
		if !s.userHasLibraryAction(r.Context(), u, id, action) {
			denyLibraryAction(w, action, "artist not found")
			return false
		}
	}
	return true
}

func (s *Server) artistLibraryIDs(ctx context.Context, artistID uuid.UUID) []uuid.UUID {
	if s == nil || s.Pool == nil || artistID == uuid.Nil {
		return nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT lib FROM (
			SELECT t.library_id AS lib
			FROM track_artists ta
			JOIN tracks t ON t.id = ta.track_id
			WHERE ta.artist_id = $1 AND t.library_id IS NOT NULL
			UNION
			SELECT a.library_id AS lib
			FROM album_artists aa
			JOIN albums a ON a.id = aa.album_id
			WHERE aa.artist_id = $1 AND a.library_id IS NOT NULL
		) x`, artistID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (s *Server) userCanStreamTrack(ctx context.Context, u *auth.User, trackID uuid.UUID) bool {
	if u != nil && u.IsAdmin {
		return true
	}
	if s == nil || s.Pool == nil || trackID == uuid.Nil {
		return false
	}
	var libID uuid.UUID
	if err := s.Pool.QueryRow(ctx, `SELECT library_id FROM tracks WHERE id=$1`, trackID).Scan(&libID); err != nil {
		return false
	}
	return s.userHasLibraryAction(ctx, u, libID, "stream")
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
