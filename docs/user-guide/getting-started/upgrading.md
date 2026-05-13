# Upgrading

Cooker is pre-1.0. The current upgrade policy is "stay close to `main` and pin to a tag." This page documents what you need to know before bumping a version, what the upgrade does, and where to find the breaking-change log.

## Versioning policy

- **SemVer** for both the binary and the chart. The chart's `version` and `appVersion` track separately: `appVersion` follows the binary; `version` follows the chart itself. Bump the chart-version-only when a template changes without a binary change.
- **Rolling minors.** Until v1.0, every minor (`0.X.0`) may include breaking changes. Patch releases (`0.X.Y`) are bug-fix-only.
- **No LTS commitment** until v1.0.
- **Security patches** back-ported to the last two minors.

The full versioning rationale and the gap to "actually shipping releases" is in [`docs/shipping-go.md` §1](../../shipping-go.md#1-release-engineering). At the time of writing Cooker does not have a tagged release stream or a published Docker image; both are tracked as `shipping-go 0-30d`.

## What an upgrade does

When a new version starts:

1. `Config.Load()` reads env vars; new fields take their default value.
2. `Config.Validate()` runs (in production). New required fields fail here loudly.
3. `postgres.NewStore` opens a connection and runs **embedded migrations** under `pg_advisory_lock(847263)` so concurrent boots cannot half-apply schema changes.
4. The server starts on `:8080`.

Embedded migrations means there is no separate "migration job" to run. Booting the new binary IS the migration.

> **Currently NO version downgrade guard.** If you boot an old binary against a newer schema, the boot succeeds and any new columns are silently ignored. This is `shipping-go 30-90d` item #4 (refuse to start if `schema_version > binary_version`).

## Skip-version policy

You can skip patch versions freely. Skipping minors is **best-effort** today — migrations are individually idempotent and ordered, so a `0.1.x -> 0.5.x` jump should work as long as no migration was removed. Removed migrations are a breaking change and would be called out in `CHANGELOG.md`.

When in doubt, the safe path is to upgrade one minor at a time.

## Pre-flight checklist

Before bumping a production install:

- [ ] Read `CHANGELOG.md` between your current version and the target.
- [ ] Take a Postgres backup (`pg_dump`, `pg_basebackup`, or your managed-DB snapshot). See [Postgres backup](../operations/postgres.md#backup-and-restore).
- [ ] Confirm no new required env var is missing in your Helm values.
- [ ] If you use the `database` secrets backend, confirm `COOKER_SECRET_KEY` hasn't changed since the last boot — rotating the key without a re-encrypt step makes existing secrets undecryptable.

## Helm upgrade

```sh
# 1. Update the chart locally (or pull from your registry once chart
#    publishing is wired up — tracked in shipping-go 30-90d).
git pull

# 2. Diff the new values against your existing release.
helm diff upgrade cooker deploy/helm/cooker/ -n cooker \
  --set cookerEnv=production \
  -f your-values.yaml

# 3. Apply.
helm upgrade cooker deploy/helm/cooker/ -n cooker \
  --set cookerEnv=production \
  -f your-values.yaml

# 4. Watch the rollout.
kubectl -n cooker rollout status deploy/cooker --timeout=300s
```

If the rollout stalls, roll back:

```sh
helm rollback cooker -n cooker
```

Postgres schema is not rolled back by Helm — for that, you need a migration-down. Migration-down semantics aren't yet exposed via a CLI subcommand (tracked in `shipping-go 30-90d #13`); for now the `down` SQL files live at `backend/internal/store/postgres/migrations/*.down.sql` if you need to roll back manually.

## Docker compose upgrade

```sh
make uat-down
git pull
make uat-up
```

`make uat-down` wipes volumes, so this is a from-scratch reset. To preserve state across upgrades use a regular `docker compose down && docker compose up -d` against `docker-compose.yml` (not the UAT one).

## Common upgrade gotchas

1. **`COOKER_SECRET_KEY` rotation.** There is no dual-key path today. Rotating the key invalidates every previously sealed secret in the database. Plan a one-shot re-encrypt step before changing it. Tracked as `S26-05-08`.
2. **CORS tightening between versions.** Production CORS defaults are deny-all. If a new minor renames a config and the chart bumps a default, double-check `COOKER_ALLOWED_ORIGINS` is still set.
3. **OIDC issuer URL changes.** Trailing slash matters — `https://auth.example.com/` and `https://auth.example.com` are different issuers to `go-oidc`. Match exactly what the IdP's `.well-known/openid-configuration` says.
4. **Replica count + memory-backed state.** Bumping `replicaCount` past 1 will fail `Config.Validate()` unless you also enable Redis backends or sticky sessions. See [Helm install: Multi-replica](helm-install.md#multi-replica).

## Cross-references

- **[`CHANGELOG.md`](https://github.com/santapong/cooker/blob/main/CHANGELOG.md)** — version history.
- **[Postgres](../operations/postgres.md)** — backup and the boot-time migration story.
- **[`docs/shipping-go.md`](../../shipping-go.md)** — why there isn't an `UPGRADING.md` yet, and when there will be.
