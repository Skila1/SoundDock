routes:
  - method: GET
    path: /api/v1/radio
    handler: Server.getRadio
    auth: requireAuth
  - method: GET
    path: /api/v1/radio/seeds
    handler: Server.radioSeeds
    auth: requireAuth
  - method: POST
    path: /api/v1/radio/refresh
    handler: Server.radioRefresh
    auth: requireAuth
  - method: GET
    path: /api/v1/playlists/folders
    handler: Server.listPlaylistFolders
    auth: requireAuth
  - method: GET
    path: /api/v1/playlists/invite
    handler: Server.previewPlaylistInvite
    auth: requireAuth
  - method: POST
    path: /api/v1/playlists/invite/accept
    handler: Server.acceptPlaylistInvite
    auth: requireAuth
  - method: POST
    path: /api/v1/playlists/{id}/invite
    handler: Server.createPlaylistInvite
    auth: requireAuth
  - method: GET
    path: /api/v1/playlists/{id}/collaborators
    handler: Server.listPlaylistCollaborators
    auth: requireAuth
  - method: DELETE
    path: /api/v1/playlists/{id}/collaborators/{userID}
    handler: Server.removePlaylistCollaborator
    auth: requireAuth
  - method: GET
    path: /api/v1/playlists/{id}/snapshots
    handler: Server.listPlaylistSnapshots
    auth: requireAuth
  - method: POST
    path: /api/v1/playlists/{id}/snapshots
    handler: Server.createPlaylistSnapshot
    auth: requireAuth
  - method: GET
    path: /api/v1/playlists/{id}/snapshots/{sid}
    handler: Server.getPlaylistSnapshot
    auth: requireAuth
  - method: POST
    path: /api/v1/playlists/{id}/snapshots/{sid}/restore
    handler: Server.restorePlaylistSnapshot
    auth: requireAuth
  - method: DELETE
    path: /api/v1/playlists/{id}/snapshots/{sid}
    handler: Server.deletePlaylistSnapshot
    auth: requireAuth
  - method: GET
    path: /api/v1/playlists/{id}/smart
    handler: Server.getSmartPlaylist
    auth: requireAuth
  - method: PUT
    path: /api/v1/playlists/{id}/smart
    handler: Server.putSmartPlaylist
    auth: requireAuth
  - method: POST
    path: /api/v1/playlists/{id}/smart/refresh
    handler: Server.refreshSmartPlaylist
    auth: requireAuth
  - method: GET
    path: /api/v1/playlists/{id}/sync-diff
    handler: Server.playlistSyncDiff
    auth: requireAuth
jobs:
  - name: radio.refresh
    payload:
      kind: artist
      seed_id: "00000000-0000-4000-8000-000000000030"
      limit: 50
  - name: smart_playlist.refresh
    payload:
      playlist_id: "00000000-0000-4000-8000-000000000090"
settings: []
dependencies: []
frontend_routes:
  - path: /radio
    component: web/src/features/playlists/RadioPage.tsx#RadioPage
  - path: /radio/:kind
    component: web/src/features/playlists/RadioStationPage.tsx#RadioStationPage
  - path: /radio/:kind/:seedId
    component: web/src/features/playlists/RadioStationPage.tsx#RadioStationPage
  - path: /playlists/invite
    component: web/src/features/playlists/PlaylistInvitePage.tsx#PlaylistInvitePage
nav: []
api_types:
  - name: RadioResponse
    fields: { kind: string, seed_id: uuid, track_ids: "uuid[]" }
  - name: RadioQuery
    fields: { kind: string, seed_id: uuid, limit: int, genre: string, decade: int }
  - name: SmartRules
    fields: { limit: int, match: "all|any", sort: "random|recent|title|year|most_played", clauses: "[{field,op,value}]" }
notes:
  - Register GET /playlists/folders and /playlists/invite before /playlists/{id}.
  - cmd/sounddock: runner.Register("radio.refresh", radio.RefreshHandler(pool)); runner.Register("smart_playlist.refresh", radio.SmartRefreshHandler(pool)).
  - P4 selects track_ids only; clients enqueue with POST /api/v1/me/queue/add. Do not write playback_sessions.
  - getPlaylist ACL stays owner OR public OR collaborator.
  - Existing playlist CRUD, reorder, M3U, unmatched/match routes unchanged.
  - Additive radio query: genre (name) and decade (year) for kinds that are not UUID seeds.
  - P6/integrator can add Start radio menu items linking to /radio?kind=artist&seed_id= (no new sidebar root).
  - If library_ux.go still defines playlist handlers, keep playlists_api.go only (Wave 0 split).
