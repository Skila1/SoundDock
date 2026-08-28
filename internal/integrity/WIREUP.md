# P3 ingest + library health wire-up

Integrator only: do not add modules. `dependencies` stays empty.

```yaml
routes:
  - method: GET
    path: /api/v1/tracks/{id}/waveform
    handler: Server.getTrackWaveform
    auth: user
    notes: Returns stored peaks or null. Never generates inline. Playback must not wait.
  - method: GET
    path: /api/v1/admin/library/health
    handler: Server.libraryHealth
    auth: admin
  - method: GET
    path: /api/v1/admin/library/settings
    handler: Server.libraryIngestSettings
    auth: admin
  - method: PUT
    path: /api/v1/admin/library/settings
    handler: Server.libraryPutIngestSettings
    auth: admin
  - method: GET
    path: /api/v1/admin/library/orphans
    handler: Server.libraryOrphans
    auth: admin
    query: library_id
  - method: GET
    path: /api/v1/admin/library/files-removed
    handler: Server.libraryFilesRemoved
    auth: admin
    query: library_id, limit
  - method: POST
    path: /api/v1/admin/library/integrity/scan
    handler: Server.libraryIntegrityScan
    auth: admin
    body: { library_id: uuid }
  - method: POST
    path: /api/v1/admin/library/files/{id}/trash
    handler: Server.libraryTrashFile
    auth: admin
  - method: POST
    path: /api/v1/admin/library/files/{id}/restore
    handler: Server.libraryRestoreFile
    auth: admin
    errors: { 409: original storage key is not free }
  - method: GET
    path: /api/v1/admin/library/duplicates
    handler: Server.libraryDuplicateGroups
    auth: admin
    notes: Extra groups API. Existing GET /api/v1/duplicates stays in handlers.go.

jobs:
  - name: waveform.generate
    handler: waveform.New(pool, srv.ProviderFor).Handler()
    payload:
      track_id: uuid
      track_file_id: uuid
    example:
      track_id: "00000000-0000-4000-8000-000000000050"
      track_file_id: "00000000-0000-4000-8000-000000000060"
  - name: fingerprint.generate
    handler: fingerprint.New(pool, srv.ProviderFor).Handler()
    payload:
      track_id: uuid
      track_file_id: uuid
    example:
      track_id: "00000000-0000-4000-8000-000000000050"
      track_file_id: "00000000-0000-4000-8000-000000000060"
  - name: integrity.scan
    handler: integrity.New(pool, srv.ProviderFor).Handler()
    payload:
      library_id: uuid
    example:
      library_id: "00000000-0000-4000-8000-000000000020"

settings:
  metadata_external_enabled: { type: bool, default: false, notes: MusicBrainz only when true }
  waveform_enabled: { type: bool, default: true }
  fingerprint_enabled: { type: bool, default: true }
  watch_enabled: { type: bool, default: false }
  auto_rescan_enabled: { type: bool, default: false }
  inbox_watch_enabled: { type: bool, default: false }
  keep_original: { type: bool, default: false }
  keep_original.{library_id}: { type: bool, default: false }
  compression_preset: { type: string, default: standard, values: [standard, high, fast] }

dependencies: []

frontend_routes:
  - path: /admin/library-health
    auth: admin
    notes: No AdminPages.tsx in this agent. Integrator mounts a page that calls the admin library APIs.

nav:
  - label: Library health
    href: /admin/library-health
    parent: Admin
    admin: true

api_types:
  LibraryHealth:
    fingerprint: '"available" | "missing"'
    ffmpeg: bool
    ffprobe: bool
    waveform_enabled: bool
    fingerprint_enabled: bool
    watch_enabled: bool
    auto_rescan_enabled: bool
    inbox_watch_enabled: bool
    keep_original: bool
    compression_preset: string
    metadata_external_enabled: bool
    trashed_files: int
    duplicate_groups: int
  WaveformResponse:
    track_id: uuid
    peaks: object | null
    ready: bool
  TrashRestore:
    ok: bool
    storage_key: string
  IntegrityScanAccepted:
    ok: bool
    job_id: uuid
    job: integrity.scan
  DuplicateGroup:
    id: uuid
    method: string
    created_at: string
    files: object[]
  FilesRemoved:
    files:
      - id: uuid
        track_id: uuid
        storage_key: string
        size_bytes: int
        deleted_at: string

notes:
  - Register job handlers in cmd/sounddock/main.go (integrator). Frozen names only waveform.generate, fingerprint.generate, integrity.scan.
  - Start watch.New(pool, scanner, srv.ProviderFor, log).Run(ctx) on the worker; it no-ops unless watch/inbox/auto-rescan settings are on. Skip read_only libraries.
  - Mount GET /api/v1/tracks/{id}/waveform next to other track routes (auth). Mount admin library routes inside requireAdmin /admin.
  - Do not add fpcalc to the image. fingerprint.generate no-ops when fpcalc is missing. Health fingerprint is available|missing.
  - ingestFile queues waveform.generate and fingerprint.generate; it never generates peaks inline.
  - Hash storage remains uploads/{hash[:2]}/{hash}{ext}. quality=original is the stream row. Keep-original copies use quality=compressed under compressed/.
  - organise.Apply / storage_key rewrite only when organisation_mode=managed AND allow_physical_reorganise. Never on the default virtual library.
  - Trash moves storage_key to trash/{file_id}/{original_key} so UNIQUE(library_id, storage_key) holds. Restore 409 if original is taken.
  - Scan skips trash/ and compressed/. WAV→FLAC still deletes the WAV unless keep-original is on for that library.
  - fingerprint.EnsureSchema creates track_fingerprints if missing (no go.mod / no extra migration from this agent).
  - ParseAudioName, IngestKey(..., originalName), and hash-title repair are unchanged.
```
