routes:
  - method: GET
    path: /api/v1/announcement
    handler: Server.publicAnnouncement
    auth: requireAuth
  - method: GET
    path: /api/v1/admin/health/detail
    handler: Server.adminHealthDetail
    auth: requireAdmin
  - method: GET
    path: /api/v1/admin/announcement
    handler: Server.adminAnnouncementGet
    auth: requireAdmin
  - method: PUT
    path: /api/v1/admin/announcement
    handler: Server.adminAnnouncementPut
    auth: requireAdmin
  - method: GET
    path: /api/v1/admin/maintenance
    handler: Server.adminMaintenanceGet
    auth: requireAdmin
  - method: PUT
    path: /api/v1/admin/maintenance
    handler: Server.adminMaintenancePut
    auth: requireAdmin
  - method: GET
    path: /api/v1/admin/quotas
    handler: Server.adminQuotasGet
    auth: requireAdmin
  - method: PUT
    path: /api/v1/admin/quotas
    handler: Server.adminQuotasPut
    auth: requireAdmin
  - method: GET
    path: /api/v1/admin/libraries/{id}/grants
    handler: Server.adminLibraryGrantsGet
    auth: requireAdmin
  - method: POST
    path: /api/v1/admin/libraries/{id}/grants
    handler: Server.adminLibraryGrantAdd
    auth: requireAdmin
  - method: DELETE
    path: /api/v1/admin/libraries/{id}/grants/{grantID}
    handler: Server.adminLibraryGrantDelete
    auth: requireAdmin
  - method: GET
    path: /api/v1/admin/backups/{id}/preview
    handler: Server.adminBackupPreview
    auth: requireAdmin
  - method: POST
    path: /api/v1/admin/backups/{id}/restore
    handler: Server.adminBackupRestore
    auth: requireAdmin
  - method: GET
    path: /api/v1/admin/diagnostics
    handler: Server.adminDiagnostics
    auth: requireAdmin
  - method: GET
    path: /api/v1/admin/demo
    handler: Server.adminDemoGet
    auth: requireAdmin
  - method: POST
    path: /api/v1/admin/demo
    handler: Server.adminDemoSeed
    auth: requireAdmin
  - method: DELETE
    path: /api/v1/admin/demo
    handler: Server.adminDemoUnseed
    auth: requireAdmin
jobs: []
settings:
  - maintenance
  - announcement
  - quotas
dependencies: []
frontend_routes:
  - path: /admin
    component: web/src/features/admin/AdminDashboard.tsx#AdminDashboard
    note: health, diagnostics, database, maintenance mode, and announcement
  - path: /admin/activity
    component: web/src/features/admin/activity/AdminActivity.tsx#AdminActivity
    note: Live, Jobs & Workers, Logs, Audit, Errors tabs (?tab=)
  - path: /admin/users
    component: web/src/features/admin/AdminUsers.tsx#AdminUsers
    note: per-user and default upload quotas
  - path: /admin/libraries
    component: web/src/features/admin/AdminLibraries.tsx#AdminLibraries
    note: libraries, storage, library grants (?tab=access), library quotas
nav:
  - source: web/src/features/admin/adminNav.ts
    note: old admin URLs redirect through legacyAdminRedirects
api_types:
  - name: Announcement
    fields: { announcement: string, maintenance: boolean }
  - name: Quotas
    fields: { default_user_bytes: number, default_library_bytes: number, users: "QuotaUserCap[]", libraries: "QuotaLibraryCap[]", library_usage: "Record<string, number>", user_usage: "Record<string, number>" }
  - name: LibraryGrant
    fields: { id: string, kind: "role|user", actions: "string[]", username: "string|null", role: "string|null" }
  - name: BackupPreview
    fields: { id: string, tables: "string[]", warnings: "string[]", restore_kind: string, verified: boolean }
notes:
  - Mount Server.MaintenanceGuard on the chi router after /healthz (and preferably after /readyz) so liveness stays 200.
  - Maintenance 503s user/admin mutations (library, users, config, uploads, metadata write-back, deletes). Always allow queue/listen/scrobble/stream/login and PUT /api/v1/admin/maintenance.
  - Per-user grants INSERT alongside role grants; DELETE refuses role_id rows.
  - Demo library is POST /api/v1/admin/demo only; never call seedDemoLibrary from main/boot.
  - Upload/ingest should call Server.CheckQuota before accepting bytes.
  - AppShell banner: GET /api/v1/announcement (requireAuth).
  - Fingerprint health is fpcalc LookPath: available|missing.
  - Restore requires JSON {confirm: true}; refuses incomplete pg_dump fallbacks.
