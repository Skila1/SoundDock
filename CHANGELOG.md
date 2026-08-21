# Changelog

## 0.0.8

- Updates page shows the new version and changelog before you install.
- Update now only signals the host helper, which runs `docker compose pull` then `docker compose up -d`.
- A progress bar tracks the image pull. SoundDock stays up until the new container starts. Postgres is not recreated.
- WAV and AIFF uploads are stored as FLAC. Files that are already compressed are left alone.
- Bulk upload runs up to 100 files at a time.

## 0.0.7

- Index uploaded audio as soon as it lands, including zip archives.
- Scan after upload no longer fails on a missing job id, so Home and Tracks can show large libraries.

## 0.0.6

- Stop the Updates page sticking on Updating when a host request file is left behind.

## 0.0.5

- Bulk file, folder, and zip uploads plus multi-URL remote import.

## 0.0.4

- Upload and URL import work without picking a library first, and only accept audio files.

## 0.0.3

- Fix admin user ids and Discord usernames, and apply in-app updates without recreating Postgres.

## 0.0.1

- User management, host systemd updates, and the first numbered release.
