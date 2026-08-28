# Backups and disaster recovery

SoundDock writes **encrypted** instance archives. A recovery passphrase is required before any backup, including the nightly job. The passphrase is never stored in the database, logs, archive manifest, or UI read APIs.

## What is packed

Inner archive (then stream-encrypted):

- Postgres dump (`pg_dump`; backup **fails** if the tool is missing)
- Checksums (compared after decrypt)
- Managed media, artwork cache, on-disk lyrics
- `restore-requirements.json` (names and classification only; no secrets, no `.env`)

NAS / `SD_LIBRARY_HOST` trees are **not** packed. Remount the same paths on the new host.

## Recovery passphrase

Set it under **Admin → Backups** (minimum 12 characters). After the first set, you can download a one-time reminder that does **not** contain the secret. Skipping the reminder still leaves the passphrase only in your keeping.

Nightly and manual backups unwrap the archive DEK with the live master key and copy the stored `recovery.box` into the clear header. They do not prompt.

Changing the passphrase re-wraps future backups. **Old backups stay recoverable with the old passphrase.**

## Restore

1. Enter the recovery passphrase.
2. SoundDock decrypts, verifies checksums, then wipes and applies. Wipe does not run if the passphrase is wrong or the archive is corrupt.
3. The master key is written to `/data/master.key` (this file wins over `SD_MASTER_KEY`).
4. The process restarts. Review **Restore requirements** for host values still needed (`SD_PUBLIC_URL`, `SD_LIBRARY_HOST`, and any env-only secrets).

A schema newer than this image is refused.

On first setup, **Restore backup** can list and import R2 archives before an administrator exists.

## Image tools

The published image includes `postgresql-client` (`pg_dump` / `psql`).
