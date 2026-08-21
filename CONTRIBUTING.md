# Contributing

SoundDock is AGPL-3.0-or-later.

1. Open an issue for significant design changes.
2. Keep Discord code in `internal/discord` — core packages must not import it.
3. The web UI must use public `/api/v1` endpoints only.
4. Add tests for auth, path sandbox, SSRF, RBAC, stream tokens, and search parsing.
5. Never add YouTube/Spotify rippers or DRM circumvention.

```bash
go test ./...
cd web && npm test || true
```
