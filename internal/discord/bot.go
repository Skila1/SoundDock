package discordx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/mediabusy"
	"github.com/sounddock/sounddock/internal/minilib"
	"github.com/sounddock/sounddock/internal/playback"
	"github.com/sounddock/sounddock/internal/scrobble"
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
	scrobble *scrobble.Service
	log      *slog.Logger
	public   string

	mu     sync.Mutex
	cancel context.CancelFunc

	sessMu sync.Mutex
	sess   *discordgo.Session

	voices sync.Map // guildID -> *guildRuntime

	gwOn int32

	rendererID      string
	generation      int64
	resumeOnRestart bool
	lastBindRev     sync.Map // guildID -> int64

	MediaBusy *mediabusy.Set
}

var (
	liveMu sync.RWMutex
	live   *Bot
)

// Live returns the process-local bot started by Run, or nil.
func Live() *Bot {
	liveMu.RLock()
	defer liveMu.RUnlock()
	return live
}

func setLive(b *Bot) {
	liveMu.Lock()
	defer liveMu.Unlock()
	live = b
}

func New(pool *pgxpool.Pool, box *cryptox.Box, se *search.Engine, play *playback.Engine, log *slog.Logger,
	provider func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) *Bot {
	b := &Bot{
		pool: pool, box: box, search: se, play: play, log: log, provider: provider,
		scrobble:        scrobble.New(pool, box, se),
		public:          strings.TrimRight(os.Getenv("SD_PUBLIC_URL"), "/"),
		resumeOnRestart: initResumeOnRestart(),
	}
	b.resetRendererIdentity()
	return b
}

func (b *Bot) Run(ctx context.Context) error {
	b.resetRendererIdentity()
	b.lastBindRev.Range(func(k, _ any) bool {
		b.lastBindRev.Delete(k)
		return true
	})
	cctx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	b.cancel = cancel
	b.mu.Unlock()
	setLive(b)
	defer func() {
		liveMu.Lock()
		if live == b {
			live = nil
		}
		liveMu.Unlock()
	}()
	_ = ensureRuntimeSchema(cctx, b.pool)
	_ = scrobble.EnsureSchema(cctx, b.pool)

	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	go b.gatewayLoop(cctx)
	b.tick(cctx)
	for {
		select {
		case <-cctx.Done():
			b.closeSession()
			return nil
		case <-t.C:
			b.tick(cctx)
		}
	}
}

func (b *Bot) Stop() {
	b.mu.Lock()
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *Bot) tick(ctx context.Context) {
	enabled, token, appID, cmdStatus, err := b.loadSettings(ctx)
	if err != nil || !enabled || token == "" {
		_, _ = b.pool.Exec(ctx, `UPDATE discord_settings SET last_gateway_status='disconnected' WHERE id=1`)
		return
	}
	if cmdStatus == "pending" || cmdStatus == "unknown" || cmdStatus == "error" {
		if err := b.syncCommands(ctx, token, appID); err != nil {
			b.log.Warn("discord command sync", "err", err)
			_, _ = b.pool.Exec(ctx, `UPDATE discord_settings SET command_registration_status='error', last_error_redacted=$1 WHERE id=1`, redacted(err.Error()))
		} else {
			_, _ = b.pool.Exec(ctx, `UPDATE discord_settings SET command_registration_status='ok', last_error_redacted=NULL WHERE id=1`)
		}
	}
	if b.session() != nil {
		_, _ = b.pool.Exec(ctx, `UPDATE discord_settings SET last_gateway_status='connected', last_error_redacted=NULL WHERE id=1`)
	}
	b.reconcileVoice(ctx)
	b.heartbeatHeldLeases(ctx)
}

func (b *Bot) loadSettings(ctx context.Context) (enabled bool, token string, appID *string, cmdStatus string, err error) {
	var enc []byte
	err = b.pool.QueryRow(ctx, `SELECT enabled, bot_token_enc, application_id, command_registration_status FROM discord_settings WHERE id=1`).
		Scan(&enabled, &enc, &appID, &cmdStatus)
	if err != nil {
		return false, "", nil, "", err
	}
	if !enabled || len(enc) == 0 || b.box == nil {
		return enabled, "", appID, cmdStatus, nil
	}
	plain, err := b.box.Decrypt(enc)
	if err != nil {
		_, _ = b.pool.Exec(ctx, `UPDATE discord_settings SET last_error_redacted='token decrypt failed', last_gateway_status='error' WHERE id=1`)
		return true, "", appID, cmdStatus, err
	}
	return enabled, string(plain), appID, cmdStatus, nil
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
		{Name: "volume", Description: "Set volume 0-100", Type: 1, Options: []any{map[string]any{"name": "level", "description": "Volume 0-100", "type": 4, "required": true, "min_value": 0, "max_value": 100}}},
		simple("clear", "Clear queue"),
		{Name: "playlist", Description: "Playlist commands", Type: 1, Options: []any{
			map[string]any{"type": 1, "name": "list", "description": "List playlists"},
			map[string]any{"type": 1, "name": "play", "description": "Play a playlist", "options": []any{map[string]any{"name": "name", "description": "Playlist name", "type": 3, "required": true}}},
		}},
	}
}

func globalCommandsURL(appID string) string {
	return "https://discord.com/api/v10/applications/" + appID + "/commands"
}

func guildCommandsURL(appID, guildID string) string {
	return "https://discord.com/api/v10/applications/" + appID + "/guilds/" + guildID + "/commands"
}

type commandPut struct {
	URL  string
	Body []byte
}

// commandSyncPuts registers one global catalogue and wipes every guild copy.
// Guild + global with the same names shows every slash command twice.
func commandSyncPuts(appID string, guildIDs []string) []commandPut {
	body, _ := json.Marshal(commands())
	out := []commandPut{{URL: globalCommandsURL(appID), Body: body}}
	empty := []byte("[]")
	for _, id := range guildIDs {
		if strings.TrimSpace(id) == "" {
			continue
		}
		out = append(out, commandPut{URL: guildCommandsURL(appID, id), Body: empty})
	}
	return out
}

func (b *Bot) syncCommands(ctx context.Context, token string, appID *string) error {
	if appID == nil || *appID == "" {
		return fmt.Errorf("no application id")
	}
	guilds, err := listBotGuilds(ctx, token)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(guilds))
	for _, g := range guilds {
		_, _ = b.pool.Exec(ctx, `INSERT INTO discord_guilds (id, name) VALUES ($1,$2) ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name`, g.ID, g.Name)
		ids = append(ids, g.ID)
	}
	var last error
	for i, op := range commandSyncPuts(*appID, ids) {
		if err := putDiscordJSON(ctx, token, op.URL, op.Body); err != nil {
			if i == 0 {
				return fmt.Errorf("global commands: %w", err)
			}
			b.log.Warn("guild command wipe", "url", op.URL, "err", err)
			last = err
		}
	}
	return last
}

type restGuild struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func listBotGuilds(ctx context.Context, token string) ([]restGuild, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://discord.com/api/v10/users/@me/guilds", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord HTTP %d", resp.StatusCode)
	}
	var guilds []restGuild
	if err := json.Unmarshal(raw, &guilds); err != nil {
		return nil, err
	}
	return guilds, nil
}

func putDiscordJSON(ctx context.Context, token, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(string(body)))
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
	if err := b.enforceQueueLimit(ctx, guildID, ids); err != nil {
		return err
	}
	if err := b.rejectExplicit(ctx, guildID, ids); err != nil {
		return err
	}
	sid, err := b.ensureBoundSession(ctx, guildID, b.voiceChannelForGuild(guildID))
	if err != nil {
		return err
	}
	if err := b.claimDiscordForCommand(ctx, sid); err != nil && !errors.Is(err, playback.ErrLeaseConflict) {
		return err
	}
	uid := minilib.LinkedUserID(ctx, b.pool, discordUser)
	ctx = playback.WithRequester(ctx, uid, strings.TrimSpace(discordUser))
	ctx = playback.WithOrigin(ctx, playback.OriginUser)
	if err := b.play.Replace(ctx, sid, ids, 0); err != nil {
		return err
	}
	return nil
}

func (b *Bot) enforceQueueLimit(ctx context.Context, guildID string, ids []uuid.UUID) error {
	var limit int
	_ = b.pool.QueryRow(ctx, `SELECT queue_limit FROM discord_guilds WHERE id=$1`, guildID).Scan(&limit)
	if limit > 0 && len(ids) > limit {
		return fmt.Errorf("queue exceeds guild limit (%d)", limit)
	}
	return nil
}

func (b *Bot) rejectExplicit(ctx context.Context, guildID string, ids []uuid.UUID) error {
	var policy string
	_ = b.pool.QueryRow(ctx, `SELECT explicit_policy FROM discord_guilds WHERE id=$1`, guildID).Scan(&policy)
	if policy != "deny" && policy != "block" {
		return nil
	}
	for _, id := range ids {
		var ex bool
		_ = b.pool.QueryRow(ctx, `SELECT coalesce(explicit,false) FROM tracks WHERE id=$1`, id).Scan(&ex)
		if ex {
			return fmt.Errorf("explicit tracks are not allowed in this guild")
		}
	}
	return nil
}

func ffmpegSeekArgs(startMS int) []string {
	if startMS <= 250 {
		return nil
	}
	return []string{"-ss", fmt.Sprintf("%.3f", float64(startMS)/1000.0)}
}

func FFmpegPCM(ctx context.Context, src string, replayGainDB float64, startMS int) (*exec.Cmd, io.ReadCloser, error) {
	args := append(ffmpegSeekArgs(startMS), "-i", src, "-f", "s16le", "-acodec", "pcm_s16le", "-ac", "2", "-ar", "48000")
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

func ffmpegPCMReader(ctx context.Context, r io.Reader, replayGainDB float64, startMS int) (*exec.Cmd, io.ReadCloser, error) {
	args := []string{"-i", "pipe:0"}
	args = append(args, ffmpegSeekArgs(startMS)...)
	args = append(args, "-f", "s16le", "-acodec", "pcm_s16le", "-ac", "2", "-ar", "48000")
	if replayGainDB != 0 {
		args = append(args, "-af", fmt.Sprintf("volume=%.3fdB", replayGainDB))
	}
	args = append(args, "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdin = r
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return cmd, stdout, nil
}

func (b *Bot) guildLibraries(ctx context.Context, guildID string) []uuid.UUID {
	rows, err := b.pool.Query(ctx, `SELECT library_id FROM discord_guild_libraries WHERE guild_id=$1`, guildID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func (b *Bot) linkedUserID(ctx context.Context, discordUser string) (uuid.UUID, bool) {
	var id uuid.UUID
	err := b.pool.QueryRow(ctx, `SELECT user_id FROM user_identities WHERE provider='discord' AND provider_user_id=$1`, discordUser).Scan(&id)
	return id, err == nil
}

func (b *Bot) guildEnabled(ctx context.Context, guildID string) bool {
	var en bool
	err := b.pool.QueryRow(ctx, `SELECT enabled FROM discord_guilds WHERE id=$1`, guildID).Scan(&en)
	if err != nil {
		return true
	}
	return en
}
