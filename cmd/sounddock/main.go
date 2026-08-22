package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sounddock/sounddock/internal/artwork"
	"github.com/sounddock/sounddock/internal/audit"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/backup"
	"github.com/sounddock/sounddock/internal/config"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/db"
	discordx "github.com/sounddock/sounddock/internal/discord"
	"github.com/sounddock/sounddock/internal/external"
	"github.com/sounddock/sounddock/internal/fingerprint"
	"github.com/sounddock/sounddock/internal/httpapi"
	"github.com/sounddock/sounddock/internal/httpapi/ratelimit"
	"github.com/sounddock/sounddock/internal/ingest"
	"github.com/sounddock/sounddock/internal/integrity"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/radio"
	"github.com/sounddock/sounddock/internal/retention"
	"github.com/sounddock/sounddock/internal/scan"
	"github.com/sounddock/sounddock/internal/scrobble"
	"github.com/sounddock/sounddock/internal/search"
	"github.com/sounddock/sounddock/internal/storage"
	"github.com/sounddock/sounddock/internal/transcode"
	"github.com/sounddock/sounddock/internal/update"
	"github.com/sounddock/sounddock/internal/watch"
	"github.com/sounddock/sounddock/internal/waveform"
	"github.com/sounddock/sounddock/internal/webhooks"
	"github.com/sounddock/sounddock/openapi"
	"github.com/sounddock/sounddock/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "update-swap" {
		if err := update.RunSwap(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Error("data dir", "err", err)
		os.Exit(1)
	}
	_ = os.MkdirAll(cfg.CacheDir, 0o755)
	_ = os.MkdirAll(cfg.BackupDir, 0o755)
	_ = os.MkdirAll(cfg.ManagedDir, 0o755)

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		log.Error("migrate", "err", err)
		os.Exit(1)
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	box, err := cryptox.New(cfg.MasterKey)
	if err != nil {
		log.Error("master key", "err", err)
		os.Exit(1)
	}
	auth.SyncDiscordEnv(ctx, pool, box, os.Getenv("SD_DISCORD_CLIENT_ID"), os.Getenv("SD_DISCORD_CLIENT_SECRET"), os.Getenv("SD_DISCORD_BOT_TOKEN"))

	runner := jobs.New(pool, log)
	art := artwork.New(pool, cfg.CacheDir)
	hooks := webhooks.New(pool, box, log)
	sc := scan.New(pool, art, log, hooks)
	managed, _ := storage.NewLocal("managed", cfg.ManagedDir, false)
	ing := ingest.New(pool, managed, sc, filepath.Join(cfg.CacheDir, "uploads"), 200<<20)
	play := playback.New(pool)
	se := search.New(pool)
	tx := transcode.New(pool, cfg.CacheDir, 10<<30, 2)
	bk := backup.New(pool, cfg.BackupDir, cfg.DatabaseURL)

	srv := &httpapi.Server{
		Cfg:     cfg,
		Pool:    pool,
		Auth:    auth.New(pool),
		Jobs:    runner,
		Search:  se,
		Play:    play,
		Art:     art,
		TX:      tx,
		Ingest:  ing,
		Backup:  bk,
		Audit:   audit.New(pool),
		Hooks:   hooks,
		Box:     box,
		Limit:   ratelimit.New(),
		Slots:   ratelimit.NewSlots(httpapi.DefaultRemoteConcurrency),
		Log:     log,
		SignKey: cryptox.SigningKey(cfg.MasterKey),
		Managed: managed,
	}
	if fsys, err := web.FS(); err == nil {
		srv.Web = fsys
	}
	srv.OpenAPI = openapi.Spec
	_ = update.WriteHostRunner()

	_ = fingerprint.EnsureSchema(ctx, pool)
	_ = scrobble.EnsureSchema(ctx, pool)

	runner.Register("library.scan", sc.Handler(srv.ProviderFor))
	runner.Register("ingest.url", ing.URLHandler(srv.ProviderFor))
	runner.Register("ingest.zip", ing.ZipHandler(srv.ProviderFor))
	runner.Register("library.migrate", ing.MigrateHandler(srv.ProviderFor))
	runner.Register("maintenance.retention", retention.Handler(pool))
	runner.Register("external.playlist.import", external.Handler(pool, box, hooks))
	runner.Register("external.playlist.tick", external.TickHandler(pool, runner.Enqueue))
	runner.Register("backup.run", func(ctx context.Context, job jobs.Job) error {
		_, err := bk.Run(ctx)
		return err
	})
	runner.Register("maintenance.gc-cache", func(ctx context.Context, job jobs.Job) error {
		return tx.Evict(ctx)
	})
	runner.Register("app.update.apply", func(ctx context.Context, job jobs.Job) error {
		by := "admin"
		var p struct {
			By string `json:"by"`
		}
		if json.Unmarshal(job.Payload, &p) == nil && p.By != "" {
			by = p.By
		}
		return update.Apply(ctx, pool, by)
	})
	runner.Register("app.update.check", func(ctx context.Context, job jobs.Job) error {
		_, err := update.Check(ctx, pool)
		return err
	})
	runner.Register("party.expire", playback.ExpireHandler(play))
	runner.Register("waveform.generate", waveform.New(pool, srv.ProviderFor).Handler())
	runner.Register("fingerprint.generate", fingerprint.New(pool, srv.ProviderFor).Handler())
	runner.Register("integrity.scan", integrity.New(pool, srv.ProviderFor).Handler())
	runner.Register("radio.refresh", radio.RefreshHandler(pool))
	runner.Register("smart_playlist.refresh", radio.SmartRefreshHandler(pool))

	role := resolveRole(cfg.Role)
	log.Info("starting", "role", role, "addr", cfg.HTTPAddr)

	if role == config.RoleAll || role == config.RoleApp || role == config.RoleWorker {
		runner.Start(ctx, 2)
		_, _ = runner.Enqueue(ctx, "maintenance.retention", map[string]any{})
		_, _ = runner.Enqueue(ctx, "external.playlist.tick", map[string]any{})
		go watch.New(pool, sc, srv.ProviderFor, log).Run(ctx)
		go func() {
			t := time.NewTicker(15 * time.Minute)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					_, _ = runner.Enqueue(ctx, "external.playlist.tick", map[string]any{})
					update.Tick(ctx, pool)
				}
			}
		}()
	}

	var httpSrv *http.Server
	if role == config.RoleAll || role == config.RoleApp || role == config.RoleAPI || role == config.RoleDiscord {
		httpSrv = &http.Server{Addr: cfg.HTTPAddr, Handler: srv.Router(), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			log.Info("listening", "addr", cfg.HTTPAddr, "role", role)
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("http", "err", err)
			}
		}()
	}

	if role == config.RoleAll || role == config.RoleDiscord {
		bot := discordx.New(pool, box, se, play, log, srv.ProviderFor)
		go func() {
			if err := bot.Run(ctx); err != nil {
				log.Error("discord", "err", err)
			}
		}()
		defer bot.Stop()
	}

	<-ctx.Done()
	log.Info("shutting down")
	srv.Draining = true
	shctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownWait)
	defer cancel()
	if httpSrv != nil {
		_ = httpSrv.Shutdown(shctx)
	}
	runner.Drain()
	pool.Close()
}

func resolveRole(fromEnv config.Role) config.Role {
	for _, a := range os.Args[1:] {
		switch config.Role(a) {
		case config.RoleAll, config.RoleApp, config.RoleAPI, config.RoleWorker, config.RoleDiscord:
			return config.Role(a)
		}
	}
	switch fromEnv {
	case config.RoleAll, config.RoleApp, config.RoleAPI, config.RoleWorker, config.RoleDiscord:
		return fromEnv
	default:
		return config.RoleAll
	}
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
