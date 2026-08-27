# Configuration

Lamund is configured entirely through command-line flags. There is no config
file to manage; state lives in the SQLite database.

## `lamund serve` — data plane

Serves sites on the public ports and manages TLS.

| Flag | Default | Description |
|------|---------|-------------|
| `--https` | *(off)* | TLS listen address, e.g. `:443`. Omit for HTTP-only. |
| `--acme-ca` | `staging` | `staging`, `production`, or a custom ACME directory URL. |
| `--acme-email` | — | contact email for Let's Encrypt. |
| `--acme-dir` | `/var/lib/lamund/acme` | where certificates are stored. |
| `--db` | `/var/lib/lamund/lamund.db` | SQLite database path. |
| `--public-ip` | *(auto)* | server IP used for the DNS pre-check. |
| `--pidfile` | — | write PID here (for `lamund reload`). |

Use `--acme-ca staging` while testing: the Let's Encrypt production endpoint
rate-limits certificate issuance (~50/week per registered domain).

## `lamund api` — control plane

Serves the panel and REST API.

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `127.0.0.1:8080` | listen address. Keep it on loopback. |
| `--db` | `/var/lib/lamund/lamund.db` | same database as `serve`. |
| `--secret-file` | `/var/lib/lamund/secret.key` | JWT signing key; created if absent. |
| `--token-ttl` | `24h` | access-token lifetime. |

## `lamund user` — accounts

```sh
lamund user create --username alice --admin      # prompts for password
lamund user create --username bob --max-sites 5  # a regular user with a quota
lamund user list
```

## `lamund site` — sites from the CLI

```sh
lamund site add --domain a.example.com --type static --root /srv/a
lamund site add --domain b.example.com --type proxy  --target 127.0.0.1:3000
lamund site list
lamund reload            # hot-reload the routing table (SIGHUP via --pidfile)
```

## Data directory

Everything Lamund needs to persist lives under `/var/lib/lamund`:

- `lamund.db` — sites, users, quotas, API keys, usage.
- `secret.key` — JWT signing secret (keep private; loss logs everyone out).
- `acme/` — issued certificates and ACME account keys.

Back up this directory to back up your whole install. It is mode `0700` and
owned by the `lamund` user.
