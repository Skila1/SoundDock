package discordx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/search"
	"github.com/sounddock/sounddock/internal/storage"
)

// Bot is the first-party Discord integration. Core packages must not import this.
type Bot struct {
	pool     *pgxpool.Pool
	box      *cryptox.Box
	search   *search.Engine
	play     *playback.Engine
	provider func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)
	log      *slog.Logger
	mu       sync.Mutex
	cancel   context.CancelFunc
}

func New(pool *pgxpool.Pool, box *cryptox.Box, se *search.Engine, play *playback.Engine, log *slog.Logger,
	provider func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) *Bot {
	return &Bot{pool: pool, box: box, search: se, play: play, log: log, provider: provider}
}

func (b *Bot) Run(ctx context.Context) error {
	cctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	b.tick(cctx)
	for {
		select {
		case <-cctx.Done():
			return nil
		case <-t.C:
			b.tick(cctx)
		}
	}
}

func (b *Bot) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
}

func (b *Bot) tick(ctx context.Context) {
	var enabled bool
	var enc []byte
	var appID *string
	err := b.pool.QueryRow(ctx, `SELECT enabled, bot_token_enc, application_id FROM discord_settings WHERE id=1`).Scan(&enabled, &enc, &appID)
	if err != nil || !enabled || len(enc) == 0 {
		_, _ = b.pool.Exec(ctx, `UPDATE discord_settings SET last_gateway_status='disconnected' WHERE id=1`)
		return
	}
	tok, err := b.box.Decrypt(enc)
	if err != nil {
		_, _ = b.pool.Exec(ctx, `UPDATE discord_settings SET last_error_redacted='token decrypt failed', last_gateway_status='error' WHERE id=1`)
		return
	}
	if err := b.syncCommands(ctx, string(tok), appID); err != nil {
		b.log.Warn("discord command sync", "err", err)
		_, _ = b.pool.Exec(ctx, `UPDATE discord_settings SET command_registration_status='error', last_error_redacted=$1 WHERE id=1`, redacted(err.Error()))
	} else {
		_, _ = b.pool.Exec(ctx, `UPDATE discord_settings SET command_registration_status='ok', last_gateway_status='polling', last_error_redacted=NULL WHERE id=1`)
	}
	_ = tok
}

func redacted(s string) string {
	if len(s) > 180 {
		return s[:180]
	}
	return s
}

type slashCmd struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Options     []any  `json:"options,omitempty"`
	Type        int    `json:"type"`
}

func commands() []slashCmd {
	q := []any{map[string]any{"name": "query", "description": "SoundDock search", "type": 3, "required": true, "autocomplete": true}}
	simple := func(name, desc string) slashCmd {
		return slashCmd{Name: name, Description: desc, Type: 1}
	}
	return []slashCmd{
		{Name: "play", Description: "Play or queue from your SoundDock library", Type: 1, Options: q},
		{Name: "search", Description: "Search SoundDock", Type: 1, Options: q},
		simple("pause", "Pause playback"),
		simple("resume", "Resume playback"),
		simple("skip", "Skip track"),
		simple("stop", "Stop and clear"),
		simple("queue", "Show queue"),
		simple("nowplaying", "Now playing"),
		simple("previous", "Previous track"),
		simple("shuffle", "Toggle shuffle"),
		simple("repeat", "Toggle repeat"),
		simple("leave", "Leave voice"),
		simple("join", "Join your voice channel"),
		simple("link", "Link your SoundDock account (opens web, no password in Discord)"),
		{Name: "volume", Description: "Set volume 0-100", Type: 1, Options: []any{map[string]any{"name": "level", "type": 4, "required": true}}},
		simple("clear", "Clear queue"),
		{Name: "playlist", Description: "Playlist commands", Type: 1, Options: []any{
			map[string]any{"type": 1, "name": "list", "description": "List playlists"},
			map[string]any{"type": 1, "name": "play", "description": "Play a playlist", "options": []any{map[string]any{"name": "name", "type": 3, "required": true}}},
		}},
	}
}

func (b *Bot) syncCommands(ctx context.Context, token string, appID *string) error {
	if appID == nil || *appID == "" {
		return fmt.Errorf("no application id")
	}
	body, _ := json.Marshal(commands())
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		"https://discord.com/api/v10/applications/"+*appID+"/commands", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord HTTP %d", resp.StatusCode)
	}
	return nil
}

// PlayQuery resolves SoundDock search into a guild playback session (used by gateway adapters and tests).
func (b *Bot) PlayQuery(ctx context.Context, guildID, discordUser string, q string, libs []uuid.UUID) error {
	hits, err := b.search.Search(ctx, q, []string{"track", "album", "playlist"}, libs, 8)
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		return fmt.Errorf("not found in SoundDock library")
	}
	var ids []uuid.UUID
	switch hits[0].Type {
	case "album":
		rows, err := b.pool.Query(ctx, `SELECT id FROM tracks WHERE album_id=$1 ORDER BY disc_number, track_number`, hits[0].ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
	case "playlist":
		rows, err := b.pool.Query(ctx, `SELECT track_id FROM playlist_entries WHERE playlist_id=$1 ORDER BY position`, hits[0].ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return fmt.Errorf("no matched tracks in SoundDock library for this playlist")
		}
	default:
		ids = []uuid.UUID{hits[0].ID}
	}
	sid, err := b.play.Session(ctx, "discord_guild", guildID, nil)
	if err != nil {
		return err
	}
	return b.play.Replace(ctx, sid, ids, 0)
}

func FFmpegPCM(ctx context.Context, src string, replayGainDB float64) (*exec.Cmd, io.ReadCloser, error) {
	args := []string{"-i", src, "-f", "s16le", "-acodec", "pcm_s16le", "-ac", "2", "-ar", "48000"}
	if replayGainDB != 0 {
		args = append(args, "-af", fmt.Sprintf("volume=%.3fdB", replayGainDB))
	}
	args = append(args, "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return cmd, stdout, nil
}
