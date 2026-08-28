```yaml
routes:
  - method: GET
    path: /api/v1/tracks/{id}/metadata
    handler: Server.getTrackMetadata
    auth: requireAuth
  - method: PATCH
    path: /api/v1/tracks/{id}/metadata
    handler: Server.patchTrackMetadata
    auth: requireAuth
  - method: POST
    path: /api/v1/tracks/bulk/metadata
    handler: Server.bulkTrackMetadata
    auth: requireAuth
  - method: PUT
    path: /api/v1/tracks/{id}/locks
    handler: Server.putTrackLock
    auth: requireAuth
  - method: PATCH
    path: /api/v1/albums/{id}/metadata
    handler: Server.patchAlbumMetadata
    auth: requireAuth
  - method: PATCH
    path: /api/v1/artists/{id}/metadata
    handler: Server.patchArtistMetadata
    auth: requireAuth
  - method: POST
    path: /api/v1/tracks/{id}/artwork
    handler: Server.postTrackArtwork
    auth: requireAuth
  - method: POST
    path: /api/v1/albums/{id}/artwork
    handler: Server.postAlbumArtwork
    auth: requireAuth
  - method: POST
    path: /api/v1/artists/{id}/artwork
    handler: Server.postArtistArtwork
    auth: requireAuth
jobs: []
settings: []
dependencies: []
frontend_routes:
  - path: /tracks/:id
    component: TrackPage
nav: []
api_types:
  - name: Track
    additive: { codec: string, bit_depth: number | null, sample_rate: number | null, genre: string, isrc: string, explicit: boolean | null, favourite: boolean }
  - name: TrackMetadata
    fields: { id: string, title: string, genre: string, year: number | null, lyrics: string, codec: string, bit_depth: number | null, sample_rate: number | null, play_count: number, last_played_at: string | null, write_back_supported: boolean, organisation_mode: string, locks: string[] }
  - name: SearchQuery
    additive: { played: "never | yes", last_played: "7d | >30d | YYYY-MM-DD" }
notes:
  - Do not remove handlers.go PATCH /tracks/{id} title-only or POST /tracks/bulk genre/year; extra methods live in metadata_edit.go.
  - Integrator: in Server.search wrap ctx with search.WithUser(r.Context(), currentUser(r).ID) so played/last_played filters are per-listener.
  - P3 write-back (not registered here): POST /api/v1/tracks/{id}/writeback and POST /api/v1/tracks/bulk/writeback body {ids, write_tags, write_artwork, managed_only}. UI flags call these; 404 is non-fatal.
  - HTML5 drag payload MIME application/x-sounddock-tracks JSON {track_ids:[]} plus text/plain csv; drop targets use data-playlist-id.
  - Favourite toggle stays POST /api/v1/favourites {type,id,on} including on:false.
```
