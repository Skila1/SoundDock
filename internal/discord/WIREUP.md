# P7 Discord gateway / voice + scrobble - integrator wireup

This package owns the **primary listening path** when Discord is enabled: music plays in guild voice from SoundDock library files (FFmpeg PCM), not as a browser substitute. No Lavalink, YouTube, or Spotify audio.

`main.go` and `server.go` / `go.mod` are owned by the integrator. Do not skip these hooks or slash commands never appear and HTTP routes 404.

## Dependencies (`go.mod`)

Add:

- `github.com/bwmarrin/discordgo` replaced with `github.com/yeongaori/discordgo-fork` (DAVE/E2EE). Upstream v0.29.0 is rejected by Discord voice with close 4017.
- `github.com/gorilla/websocket` and `github.com/cloudflare/circl` (transitive via the fork)

System: `ffmpeg` built with `libopus` (PCM from `FFmpegPCM` is re-encoded to Opus for voice UDP). Optional Last.fm: `SD_LASTFM_API_KEY`, `SD_LASTFM_API_SECRET`.

```
replace github.com/bwmarrin/discordgo => github.com/yeongaori/discordgo-fork v0.0.0-20260627070107-c65bda26a53b
```

## `cmd/sounddock/main.go` - run the bot on `SD_ROLE=all` as well as `discord`

Today the bot starts only when `role == config.RoleDiscord`. Also start it for `RoleAll` (single-process compose):

```go
if role == config.RoleAll || role == config.RoleDiscord {
    bot := discordx.New(pool, box, se, play, log, srv.ProviderFor)
    go func() {
        if err := bot.Run(ctx); err != nil {
            log.Error("discord", "err", err)
        }
    }()
    defer bot.Stop()
}
```

`Bot.Run` opens the Gateway, handles slash interactions, joins voice, and streams `FFmpegPCM` from `StorageProvider`. Admin **Sync commands** sets `command_registration_status='pending'`; the existing 15s tick **PUTs** the command catalogue globally and **per invited guild** when status is `pending`, `unknown`, or `error`. Ready and `GUILD_CREATE` also register guild commands so they appear immediately.

## `internal/httpapi/server.go` - mount P7 routes (do not rewrite Router)

Inside the authenticated `/api/v1` group (`requireAuth`), call:

```go
s.MountP7(r)
```

That registers:

| Method | Path | Notes |
|--------|------|--------|
| GET | `/api/v1/me/discord/voice-state` | `{discord_enabled, linked, in_voice, guild_id, channel_id}` (+ `application_id` for web RPC) |
| POST | `/api/v1/me/discord/join` | Join invoker’s current VC; `409 not_in_voice` |
| POST | `/api/v1/me/discord/play` | `{track_ids, start}` join then `playback.Engine` `discord_guild` session (P1). Do not invent a second queue. |
| POST | `/api/v1/me/discord/link` | `{challenge}` complete `/link` from Discord |
| POST | `/api/v1/me/listen` | Listen/scrobble events (`progress` / `skip`) per `contracts/listen.json` |
| GET/PUT | `/api/v1/me/scrobble` | Last.fm / ListenBrainz / Discord presence |
| POST | `/api/v1/me/scrobble/import` | `{provider: lastfm\|listenbrainz}` → `listen_history` `source='import'` only |

Same-process join uses `discordx.Live()`. If the API role has no gateway, join writes `discord_voice_runtime.last_disconnect_reason='pending_join'` and the bot tick completes it.

## Web player Rich Presence

`web/src/features/settings/discordPresence.ts` talks to Discord desktop RPC on `127.0.0.1:6463-6472`. Connected Services toggles it. To keep presence alive on every page, import in `AppShell`:

```ts
import { ensureDiscordPresence } from "@/features/settings/discordPresence";
// in the existing useEffect:
ensureDiscordPresence();
```

PlayerBar (P2) still owns Browser/Discord output toggle. Presence is not a playback destination.

## Slash catalogue

Do not invent a second command set. Keep `CommandHelp` in `commands.go`. Implemented: `/play` (auto-join + `PlayQuery`), `/search`, `/pause`, `/resume`, `/skip`, `/previous`, `/stop`, `/queue`, `/nowplaying`, `/shuffle`, `/repeat`, `/volume`, `/clear`, `/leave`, `/join` (invoker’s VC or error), `/playlist list`, `/playlist play`, `/link`.

## Env

- `SD_PUBLIC_URL` - `/link` challenge URL
- `SD_LASTFM_API_KEY` / `SD_LASTFM_API_SECRET` - Last.fm scrobble + import
- `SD_ROLE=all` or `discord` - gateway process
