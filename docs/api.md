# API

Interactive docs: `/api/docs` (OpenAPI at `/api/v1/openapi.json`).

Bot flow:

1. `GET /api/v1/system/info`
2. Authenticate (Discord OAuth session, or an API key `sd_…` as Bearer)
3. `GET /api/v1/search?q=Linkin+Park+Numb&type=track`
4. Stream with the returned `stream_url` (short-lived token). Never filesystem paths.

Admin Discord invite: `GET /api/v1/admin/integrations/discord/invite`
