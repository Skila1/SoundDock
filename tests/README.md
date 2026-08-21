# Integration tests

Run against a Compose stack:

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d
go test ./tests/integration/...
```

Unit tests covering auth, path sandbox, SSRF, search parsing, stream tokens, rate-limit classes, and RBAC live next to the packages they test (`go test ./...`).
