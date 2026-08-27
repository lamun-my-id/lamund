# Architecture

Lamund is a single Go binary. At runtime it presents two planes that share
one SQLite database.

```
            Internet
               │
        :80 / :443                     127.0.0.1:8080
               │                              │
      ┌────────▼─────────┐          ┌─────────▼──────────┐
      │   data plane     │          │   control plane    │
      │  (lamund serve)  │          │   (lamund api)     │
      │                  │          │                    │
      │ vhost routing    │          │ REST /api/v1       │
      │ static + proxy   │          │ embedded Vue panel │
      │ certmagic (TLS)  │          │ JWT / API-key auth │
      └────────┬─────────┘          └─────────┬──────────┘
               │                              │
               └───────────┬──────────────────┘
                           ▼
                   SQLite (modernc, pure-Go)
              sites · users · quotas · api_keys · usage
```

## Packages

| Package | Responsibility |
|---------|----------------|
| `internal/store` | SQLite access, migrations, per-user file isolation (`os.Root`), usage. |
| `internal/vhost` | hostname → handler table, hot-swapped atomically. |
| `internal/static` | static file serving, traversal-guarded via `os.Root`. |
| `internal/proxy` | reverse proxy with anti-SSRF target validation. |
| `internal/acme` | ACME/Let's Encrypt via certmagic, DNS pre-check. |
| `internal/auth` | argon2id passwords, JWT issuer, API-key generation. |
| `internal/api` | REST router, auth middleware, sites/admin/apikey handlers. |
| `internal/quota` | per-user site and storage limits. |
| `internal/logging` | rotating access log + bandwidth aggregation. |
| `internal/server` | builds the vhost table from the store; reloader. |
| `web` | the Vue panel and its `go:embed` glue. |
| `cmd/lamund` | CLI: `serve`, `api`, `site`, `user`, `reload`, `version`. |

## Design choices

- **Pure-Go SQLite** (`modernc.org/sqlite`) so the binary needs no CGO and
  cross-compiles to `arm64`/`amd64` cleanly. No external database process.
- **`os.Root` for isolation** (Go 1.24). Static serving and per-tenant file
  storage go through `os.Root`, which rejects path traversal at the syscall
  level — a symlink pointing outside a tenant's directory still fails.
- **Lock-free hot reload.** The routing table is an
  `atomic.Pointer[vhost.Table]`; a reload builds a new table and swaps the
  pointer, so live requests never block on config changes.
- **Two planes, one DB.** Keeping the public listener and the panel in
  separate processes means the panel can stay on loopback while the data
  plane faces the internet. SQLite in WAL mode handles the shared access.

## Request flow (data plane)

1. A request arrives on `:443`. certmagic completes the TLS handshake,
   issuing a certificate on first contact if the domain resolves here.
2. The host header is normalized and looked up in the vhost table.
3. A **static** route serves files from the site root; a **proxy** route
   forwards to the (loopback) upstream with `X-Forwarded-*` headers.
4. Unknown hosts and disabled sites get a plain Lamund error page.
