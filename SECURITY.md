# Security Policy

SoundDock is designed to be internet-exposable. Please report vulnerabilities privately.

## Do not

- Share bot tokens, master keys, or storage credentials in issues
- Expect permanent unauthenticated media URLs
- Point Remote Import at internal/metadata IPs (they are blocked)

## Hardening checklist

- Set a long random `SOUNDDOCK_MASTER_KEY`
- Use HTTPS (`SOUNDDOCK_PUBLIC_URL`, `SOUNDDOCK_COOKIE_SECURE=true`)
- Restrict `SOUNDDOCK_TRUSTED_PROXIES`
- Keep `/metrics` disabled or token-protected
- Run containers as non-root (default image)
- Backups include secrets wrapped by the master key. Store the key offline.
