routes:
  - method: GET
    path: /api/v1/me/history
    handler: Server.historyRecent
    auth: requireAuth
  - method: GET
    path: /api/v1/me/never-played
    handler: Server.neverPlayed
    auth: requireAuth
  - method: GET
    path: /api/v1/me/rediscovery
    handler: Server.rediscovery
    auth: requireAuth
  - method: GET
    path: /api/v1/me/stats
    handler: Server.listeningStats
    auth: requireAuth
  - method: GET
    path: /api/v1/me/wrapped
    handler: Server.wrapped
    auth: requireAuth
jobs: []
settings: []
dependencies: []
frontend_routes:
  - path: /history
    component: HistoryPage
    from: web/src/features/history/HistoryPage.tsx
  - path: /history/never-played
    component: NeverPlayedPage
    from: web/src/features/history/NeverPlayedPage.tsx
  - path: /history/rediscovery
    component: RediscoveryPage
    from: web/src/features/history/RediscoveryPage.tsx
  - path: /stats
    component: StatsPage
    from: web/src/features/stats/StatsPage.tsx
  - path: /wrapped
    component: WrappedPage
    from: web/src/features/wrapped/WrappedPage.tsx
nav:
  - slot: Profile submenu
    label: History
    to: /history
  - slot: Profile submenu
    label: Listening stats
    to: /stats
  - slot: Profile submenu
    label: Wrapped
    to: /wrapped
  - slot: Profile submenu
    label: Never played
    to: /history/never-played
  - slot: Profile submenu
    label: Rediscovery
    to: /history/rediscovery
api_types:
  - name: ListenTrack
    from: web/src/features/stats/types.ts
  - name: StatsResponse
    from: web/src/features/stats/types.ts
  - name: WrappedResponse
    from: web/src/features/stats/types.ts
notes:
  - Do not add five new sidebar roots; History/stats/Wrapped live under Profile submenu (or Home). In-page ListeningNav is already in the pages.
  - Recap totals default to listen.json wrapped_default_sources web+discord; source=import is a labelled sidecar and is excluded unless include_import=true.
  - Existing GET /api/v1/history (Server.history in home.go) stays; GET /api/v1/me/history is the dedicated recently-played join.
  - Query params: period=week|month|year|all on stats; year and optional month on wrapped; days=14..365 on rediscovery; sources=web,discord,import; include_import=true.
  - Home See all links to /history and /stats; Home still owns Recently added, Most played, Create playlist, LayoutToggle, and empty-library copy.
  - GET /api/v1/me/listen write path is P1; these handlers only read listen_history and play_counts.
