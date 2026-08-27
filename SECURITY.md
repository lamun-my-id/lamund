# Security Policy

## Reporting a vulnerability

Please report security issues privately. Do **not** open a public issue for
a vulnerability.

- Email: **security@lamun.my.id**
- Or use GitHub's private "Report a vulnerability" advisory on this repo.

Include the version, affected component, reproduction steps, and impact.
We aim to acknowledge within 72 hours and to ship a fix or mitigation
before any public disclosure. We will credit reporters who wish to be
named.

## Supported versions

Lamund is pre-1.0. Security fixes land on `main` and in the latest release.
Older releases are not maintained.

## Security model, in brief

- The panel/API binds loopback by default; do not expose it directly.
- Reverse-proxy targets are loopback-only unless explicitly allowed, to
  guard against SSRF.
- Tenants are isolated on disk via `os.Root`; traversal outside a tenant's
  directory is rejected at the syscall level.
- Passwords use argon2id; API keys are stored as SHA-256 hashes; the JWT
  signing secret lives in the mode-`0700` data directory.

If you find a gap in any of these, we want to hear about it.
