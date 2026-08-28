# P8 WIREUP - offline PWA + stream policy

P8 owns `web/src/offline/**`, `web/src/main.tsx` (install/update toasts), `web/vite.config.ts` (NetworkOnly `/api` `/rest`; no stream SW cache), and `internal/httpapi/stream_policy.go`. Do not parse `X-Forwarded-For` outside `proxyHeaders`.

## Routes (`internal/httpapi/server.go`)

Inside the authenticated `/api/v1` group:

```
r.Post("/me/offline/tokens", s.mintOfflineToken)
r.Delete("/me/offline/tokens", s.revokeOfflineTokens)
```

Inside the existing admin group (`requireAdmin`):

```
r.Get("/stream-policy", s.adminGetStreamPolicy)
r.Put("/stream-policy", s.adminPutStreamPolicy)
```

## Stream handler (`internal/httpapi/stream.go`)

`s.streamTrack` today authenticates interactive `?token=` or session/API key. After that:

1. If `offline_token` query is set, call `claims, err := s.VerifyOfflineToken(r.Context(), tok)`. Reject 401 on error. Require `claims.TrackID == id`. Do **not** treat this as a general API credential. Do not accept interactive `?token=` as an offline fill credential.
2. Replace (or wrap) `s.Slots.Acquire(r.RemoteAddr)` with:
   - `if !s.AcquireStreamSlot(r) { 429 stream_limit }`
   - `defer s.ReleaseStreamSlot(r)`
   Keep `s.Slots` only as a process-wide backstop if you still want one.
3. After choosing `quality`, set `quality = s.CapStreamQuality(r, quality)` so remote clients cannot exceed `stream_remote_max_bitrate` (LAN uses `stream_lan_max_bitrate`, `0` = original).
4. Read **`r.RemoteAddr` only**. `proxyHeaders` already rewrote it. If hop count or trusted CIDRs are wrong, extend `proxyHeaders` / `SD_TRUSTED_PROXIES` - do not add a second XFF parser in stream or policy code.

## `NewSlots` (`cmd/sounddock/main.go`)

```
Slots: ratelimit.NewSlots(httpapi.DefaultRemoteConcurrency),
```

`DefaultRemoteConcurrency` is `8`, matching `stream_remote_concurrency`. Admin changes to remote concurrency are enforced by `AcquireStreamSlot` (per client IP, fail-closed remote). LAN is unlimited in policy; raising `NewSlots` only matters if `stream.go` still uses `s.Slots` for every client.

## Proxy / CIDR (`proxyHeaders` only)

Trusted proxy CIDRs live in `SD_TRUSTED_PROXIES` (`config.TrustedNets`). `proxyHeaders` copies the first `X-Forwarded-For` hop when the **direct** peer is trusted. Multi-hop / Cloudflare / extra RFC1918 ranges: change that middleware and the env list. Stream policy then sees the client on `r.RemoteAddr` and fail-closes unknown IPs to remote bitrate + remote concurrency.

## Web player

`web/src/lib/api.ts` is owned elsewhere. Integrator should:

- Prefer `offlineObjectUrl(trackId)` from `@/offline` when the track is opted in (blob URL). Never set `audio.src` to a cached `/api/v1/tracks/*/stream` URL.
- Opt-in bulk download: `fillTracks(ids)` - **max 2 concurrent `/stream` fills**.
- Device revoke: `revokeDeviceAndClear()` (DELETE `/api/v1/me/offline/tokens` + Cache API wipe).
- Do not persist interactive `/stream?token=` values in Cache Storage or the service worker.

## Offline token storage

Mint/revoke live in `stream_policy.go`. Tokens are HMAC (`offline-v1|user|device|track|issued|exp`), 30-day TTL, no interactive-stream authority. Revoke writes `server_settings` key `offline_revoke:{userID}:{deviceID}` (unix watermark); tokens issued at or before that are dead. No extra migration.

## PWA / Workbox

`vite.config.ts` keeps `NetworkOnly` for `/api` and `/rest`. `navigateFallbackDenylist` is unchanged. Stream bodies are **not** Workbox-cached; offline audio uses a separate Cache (`sounddock-offline-audio-v1`) keyed at `/__offline/tracks/{id}`. `main.tsx` registers `virtual:pwa-register` and `beforeinstallprompt` toasts.
