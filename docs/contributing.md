# Contributing

Thanks for your interest in Lamund. This document covers how to build, test,
and submit changes.

## Prerequisites

- Go ≥ 1.24 (the code uses `os.Root`).
- Node ≥ 20 to build the panel.

## Build

The Go binary embeds the built panel, so build the panel first:

```sh
cd web && npm ci && npm run build && cd ..
go build ./...
```

During panel development, run Vite's dev server instead — it proxies
`/api` to a local `lamund api` on `:8080`:

```sh
cd web && npm run dev
```

## Test

```sh
go test ./...     # all packages
go vet ./...
```

The ACME test (`internal/acme`) issues a real certificate against a local
[Pebble](https://github.com/letsencrypt/pebble) server. It is skipped
automatically when Pebble is not installed; install it to run that test:

```sh
go install github.com/letsencrypt/pebble/v2/cmd/pebble@latest
```

## Conventions

- **TDD.** Write a failing test, watch it fail, implement, watch it pass.
  New behavior arrives with its test.
- **Small, focused commits** using Conventional Commit prefixes
  (`feat:`, `fix:`, `docs:`, `build:`, `ci:`).
- **Keep dependencies minimal.** The single-binary, no-CGO property is a
  feature — prefer the standard library. New dependencies need a reason.
- **Isolation is security-critical.** Changes touching `os.Root` usage,
  proxy target validation, or tenant scoping must include tests that prove
  the boundary holds.

## Pull requests

Run `go test ./...` and `go vet ./...` before opening a PR, and describe
what you changed and why. CI runs tests, vet, `golangci-lint`, and a
multi-arch build.
