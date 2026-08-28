```yaml
routes:
  - method: GET
    path: /api/v1/me/tokens
    handler: Server.listTokens
    auth: requireAuth
  - method: POST
    path: /api/v1/me/tokens
    handler: Server.createToken
    auth: requireAuth
  - method: DELETE
    path: /api/v1/me/tokens/{id}
    handler: Server.revokeToken
    auth: requireAuth
jobs: []
settings: []
dependencies: []
frontend_routes:
  - path: /profile
    component: ProfilePage
  - path: /profile/devices
    component: DevicesPage
  - path: /profile/party
    component: PartyPage
nav:
  - slot: Profile submenu
    to: /profile/devices
    label: Devices
  - slot: Profile submenu
    to: /profile/party
    label: Party
api_types:
  - name: Session
    fields: { id: string, user_agent: string | null, ip: string | null, created_at: string, last_seen_at: string, expires_at: string }
  - name: PersonalAccessToken
    fields: { id: string, name: string, prefix: string, scopes: string[], last_used_at: string | null, created_at: string, secret: string }
  - name: PartyState
    fields: { session_id: string, enabled: boolean, host_user_id: string, expires_at: string | null, members: { user_id: string, role: string }[], votes: { track_id: string, user_id: string, created_at: string }[] }
  - name: QueueState
    additive: { kind: string, owner_key: string, shuffle_mode: string, stop_after_current: boolean, device_id: string | null }
notes:
  - GET/DELETE /me/sessions already registered in handlers.go; do not duplicate.
  - PAT secrets use sdp_ prefix hashed into personal_access_tokens.secret_hash; shown once on create.
  - Integrator: on PAT auth in apiKeyUser, SET last_used_at=now() for the matching personal_access_tokens row.
  - Party/handoff UI calls P1 GET/POST /me/party, POST /me/party/votes, GET /me/queue, POST /me/queue/control only.
  - AppShell titles: /profile/devices=Devices, /profile/party=Party.
```
