# API

Interactive docs: `/api/docs` (OpenAPI at `/api/v1/openapi.json`).

Bot flow:

1. `GET /api/v1/system/info`
2. Authenticate (`/api/v1/auth/login` or an API key `sd_…`)
3. `GET /api/v1/search?q=Linkin+Park+Numb&type=track`
4. Stream with the returned `stream_url` (short-lived token) — never filesystem paths

Admin Discord invite: `GET /api/v1/admin/integrations/discord/invite`
