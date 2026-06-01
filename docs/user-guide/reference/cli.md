# CLI reference

> **Honest disclosure.** The `cooker` binary currently has **no CLI flags or subcommands**. `main.go` accepts no `--version`, no `--config-print`, no `--migrate`. Configuration is purely environment-variable-based; the binary's sole job is to start the HTTP server.
>
> This page documents (a) what does exist today, (b) what's on the [shipping-go roadmap](../../reference/shipping-go.md#1-release-engineering).

## What exists today

The binary is at `backend/cmd/cooker/main.go`. It:

1. Reads env vars via `config.Load()`.
2. Calls `config.Validate()`.
3. Starts the HTTP server via `server.New(cfg)`.
4. Handles SIGTERM / SIGINT for graceful shutdown.

There are no positional arguments. There are no flags. `cooker --help` prints the standard Go flag help (empty, since no flags are registered) and exits non-zero.

```sh
# What works today.
cooker        # starts the server, blocks until SIGTERM.

# What does NOT yet work.
cooker --version
cooker --help
cooker version
cooker config print
cooker config validate
cooker migrate up
cooker migrate status
```

## What you can do via Make targets

The `Makefile` in the repo root has the operator-side ergonomics that the binary doesn't have yet:

| Target | What it does |
|---|---|
| `make test` | `go test ./... -race` against a local Postgres service. |
| `make build` | Build the binary at `bin/cooker`. |
| `make docker-build` | Build the multi-stage Docker image. |
| `make uat-up` | Bring up the UAT compose stack. |
| `make uat-down` | Tear it down (wipes volumes). |
| `make uat-logs` | Tail the Cooker container's stdout. |
| `make uat-shell` | Shell inside the Cooker container (`kubectl`, `git`, `docker-cli` present). |
| `make uat-reset` | `down` + `up`. |
| `make swagger` | Regenerate OpenAPI from doc-comments via `swag`. |

## What's on the way (`shipping-go`)

The full plan is in [`docs/shipping-go.md`](../../reference/shipping-go.md). The operator-facing items:

### Phase 0-30 days

| Subcommand | Purpose |
|---|---|
| `cooker version` | Print the version + git SHA + build date. Wired via `-ldflags`. |
| Also: `GET /version` HTTP endpoint | Same data via the API for in-cluster checks. |

### Phase 30-90 days

| Subcommand | Purpose |
|---|---|
| `cooker config print` | Resolve full config (file + env + flags) and emit YAML with secrets redacted. The "what is this server actually running with?" command. |
| `cooker config validate <file>` | Dry-run validate a YAML config without booting. |
| `cooker migrate up` | Apply embedded migrations and exit (today this happens automatically on boot — making it explicit is the win). |
| `cooker migrate down N` | Roll back N migrations. |
| `cooker migrate status` | Print current schema version. |

Until those land, the equivalents are:

- **Version**: read the Docker image tag, or `docker inspect cooker:latest | jq '.[].Config.Labels'` if the image is OCI-labelled.
- **Config print**: there isn't one. Inspect Helm values + env vars by hand.
- **Migrate up**: happens at boot. Watch the logs.
- **Migrate down**: hand-run the `.down.sql` files in `backend/internal/store/postgres/migrations/`.
- **Migrate status**: `SELECT * FROM schema_migrations ORDER BY version DESC LIMIT 5;`.

## Other "expected" CLI features that don't exist

| Feature | Status |
|---|---|
| `cooker serve` (explicit serve mode) | Not needed — boot does this. |
| `cooker users create` | No. Local-auth signup via `POST /api/v1/auth/local/signup`. |
| `cooker users list` | No. Query `users` table directly: `SELECT id, email, roles FROM users;`. |
| `cooker secrets export` | No. Use the REST API. |
| `cooker pipelines export <id> -o yaml` | No (and the YAML format doesn't yet exist — roadmap C4). |
| `cooker --print-routes` | No. `internal/server/router.go` is the source. |

## Environment-only configuration

Until a YAML config file exists (roadmap `shipping-go 30-90d` #14), the only way to configure Cooker is via env vars. See [Environment variables](env-vars.md).

## How to verify a deployment

Without `cooker --version`, the practical "is this the version I expected" checks are:

```sh
# 1. The OCI label on the image (set by GoReleaser when releases ship).
docker inspect cooker:<tag> | jq -r '.[].Config.Labels."org.opencontainers.image.version"'

# 2. The chart's appVersion.
helm get values cooker -n cooker

# 3. The pod's started-at timestamp.
kubectl -n cooker get pod -o jsonpath='{.items[0].status.startTime}'
```

When `cooker version` lands, all three will be obsolete in favour of:

```sh
cooker version
# cooker version 0.2.1 (commit abc1234, built 2026-05-12)
```

## Cross-references

- **[`docs/shipping-go.md`](../../reference/shipping-go.md)** — the full CLI / release roadmap.
- **[Configuration](../getting-started/configuration.md)** — env-var configuration today.
- **[Postgres: migrations](../operations/postgres.md#migrations)** — what happens today instead of `cooker migrate`.
