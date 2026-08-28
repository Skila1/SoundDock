```yaml
routes: []
jobs: []
settings: []
dependencies: []
frontend_routes: []
nav: []
api_types: []
notes:
  - Mount PrefsSync from @/stores/prefs in Providers (or AppShell) so login/setup pick up --sd-accent and html.sd-compact. TopBar already mounts it while authenticated.
  - Accent is CSS var --sd-accent (and --sd-accent-hover/--sd-ring) on documentElement, default #1db954. Do not hardcode a new brand green.
  - Compact density is html.sd-compact only; not a layout engine. Optional extra rules may be added to index.css.
  - Mount QrShare/QrCode from @/lib/qr.tsx where share/pair UI belongs (login, profile, or TopBar). Do not auto-mount in Sidebar/AppShell from this wave.
  - Command palette navigation/actions are already in CommandSearch below search hits; Ctrl/Cmd+K unchanged.
```
