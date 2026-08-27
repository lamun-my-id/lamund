# Lamund

**One binary that turns a bare Linux box into a hosting platform.** Point a
domain at your server, add a site in the panel, and Lamund serves it over
HTTPS — certificates issued and renewed for you. No nginx, no Certbot, no
Docker, no database to run. Just `./lamund`.

[![License](https://img.shields.io/badge/license-Apache--2.0-4d3095)](LICENSE)

---

## Why

Standing up a small site today means wiring together a web server, a TLS
tool, a reverse proxy config, and a process manager — then keeping all four
in sync. Lamund folds that into a single Go binary with an embedded web
panel and a built-in SQLite store. It is small enough to read, and it runs
unprivileged.

- **Auto-HTTPS** — Let's Encrypt certificates issued and renewed
  automatically once a domain resolves to your server.
- **Static & reverse-proxy sites** — serve a folder, or forward to an app on
  a local port. Proxy targets are loopback-only by default.
- **Multi-user with quotas** — an admin creates users; each user manages
  their own sites within a site/storage quota. Tenants are isolated at the
  filesystem level.
- **Embedded panel** — a Vue 3 UI ships inside the binary; there is nothing
  extra to deploy.
- **REST API** — everything the panel does is available at `/api/v1`, with
  JWT or API-key auth.

## Quickstart

On a fresh Ubuntu VPS, as root:

```sh
curl -fsSL https://raw.githubusercontent.com/lamun-my-id/lamund/main/scripts/install.sh | sh
```

The installer detects your architecture, installs the binary and systemd
units, creates the `lamund` system user, and prints a one-time admin
password. The data plane binds `:80`/`:443`; the panel listens on
`127.0.0.1:8080`.

Reach the panel over an SSH tunnel:

```sh
ssh -L 8080:127.0.0.1:8080 you@your-server
# then open http://127.0.0.1:8080
```

Add a site, point its DNS `A` record at the server, and HTTPS comes up on
its own.

## From source

Requires Go ≥ 1.24 (for `os.Root`) and Node ≥ 20 (to build the panel).

```sh
cd web && npm ci && npm run build && cd ..   # build the embedded panel
go build -o lamund ./cmd/lamund
```

Then either use the CLI or run the two planes:

```sh
./lamund user create --username admin --admin        # bootstrap
./lamund serve --https :443 --acme-email you@example.com   # data plane
./lamund api --addr 127.0.0.1:8080                   # panel + REST
```

## How it fits together

Lamund runs as two processes sharing one SQLite database:

- **Data plane** (`lamund serve`) — the public listener on `:80`/`:443`. It
  routes each hostname to a static handler or a reverse proxy, and manages
  TLS via [certmagic](https://github.com/caddyserver/certmagic).
- **Control plane** (`lamund api`) — the panel and REST API on loopback.

See [docs/architecture.md](docs/architecture.md) for the full picture.

## Documentation

- [Install](docs/install.md) — installer, systemd, upgrading
- [Configuration](docs/config.md) — flags, ports, data directory
- [Architecture](docs/architecture.md) — how the pieces connect
- [Contributing](docs/contributing.md) — build, test, conventions
- [Security policy](SECURITY.md)

## Status

Pre-1.0. The core — static/proxy sites, auto-HTTPS, multi-user, quotas,
panel — is built and tested. Interfaces may still change before v1.

## License

Apache-2.0 © Lamun Research. See [LICENSE](LICENSE).
