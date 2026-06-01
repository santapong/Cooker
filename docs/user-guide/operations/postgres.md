# PostgreSQL

Cooker uses PostgreSQL as its source of truth. This page is the operator-side view: setup, migrations, backup, the boot-time advisory lock.

For schema details, see [`docs/architecture.md`](../../reference/architecture.md#database-schema).

## Setup

Bring your own Postgres. Cooker does not currently ship a Postgres subchart. Three patterns:

| Pattern | When |
|---|---|
| Managed (RDS, Cloud SQL, Aiven, Crunchy) | Production. Backups, replication, point-in-time recovery, HA — all delegated. |
| Self-managed in-cluster Postgres pod | Mid-size installs that want everything in K8s. Bitnami's `bitnami/postgresql` chart works; run it separately. |
| Local `postgres` container | UAT compose stack; dev. |

Cooker connects via `DATABASE_URL`:

```text
postgres://user:password@host:5432/database?sslmode=require
```

`Config.Validate()` (production) requires `DATABASE_URL` to be set and not equal to the dev default.

### SSL mode

Production must NOT connect to Postgres over plaintext. Append `?sslmode=require` (or `verify-full` if you mount a CA bundle into the pod).

| Mode | Behaviour |
|---|---|
| `disable` | No TLS. Don't use in production. |
| `allow` | Prefer plaintext, fall back to TLS. |
| `prefer` | Prefer TLS, fall back to plaintext. |
| `require` | Require TLS. Don't verify the cert. |
| `verify-ca` | Require TLS and verify the cert against a CA bundle. |
| `verify-full` | `verify-ca` plus check the hostname matches the cert. |

`require` is the production floor; `verify-full` is the goal.

> **Known gap.** `Config.Validate()` does NOT currently reject `?sslmode=disable` in production. Tracked as `S26-05-10` — until that fix lands, the operator must set this correctly by hand.

## Migrations

Migrations are **embedded** in the binary via `//go:embed migrations/*.up.sql` (in `backend/internal/store/postgres/`). They apply automatically at boot.

### What happens on boot

1. `postgres.NewStore` opens a connection pool.
2. It acquires a `pg_advisory_lock(847263)` so two replicas booting simultaneously cannot both attempt to apply migrations.
3. It compares the schema version in the `schema_migrations` table against the embedded migrations.
4. It applies any missing migrations in numbered order, each inside a transaction. The version-table insert is part of the same transaction, so a crashed migration leaves the schema in a consistent (pre-migration) state.
5. It releases the advisory lock.
6. The server starts.

This is idempotent. A second boot does nothing if the schema is current.

### What the migrations cover

| File | What |
|---|---|
| `001_initial.up.sql` | Pipelines, pipeline_runs, environments, registry_configs, cluster_configs. |
| `002_*.up.sql` | First-batch additions; see file headers. |
| ... | Each numbered file is one schema change. |
| `008_app_health.up.sql` | App-health columns: `health_status`, `health_checked_at`, `health_message`. |

Each migration has a paired `.down.sql` for rollback, NOT applied automatically. To roll back, run the relevant `.down.sql` by hand against the database. There is no `cooker migrate down` CLI yet (tracked in `shipping-go 30-90d` #13).

> **Operator warning.** Booting an OLDER binary against a NEWER schema currently does not refuse to start. New columns are silently ignored; queries against new tables 404. Tracked as `shipping-go 30-90d` #4 ("Refuse to start if `schema_version > binary_version`").

## Backup and restore

Cooker doesn't run backups for you. Pick one:

### Minimum-viable: `pg_dump` cron

```sh
pg_dump "$DATABASE_URL" --no-owner --clean --if-exists > cooker-$(date +%F).sql
```

Run nightly via a `CronJob`. Encrypt and ship to durable storage (S3, GCS).

### Production: WAL archiving

Use [WAL-G](https://github.com/wal-g/wal-g) or [Barman](https://pgbarman.org/) for continuous WAL shipping + point-in-time recovery. The setup is Postgres-side, not Cooker-side.

For managed Postgres (RDS, Cloud SQL), enable automated backups + PITR in the cloud console. Done.

### Restoring

The dump path:

```sh
psql "$DATABASE_URL" < cooker-2026-05-12.sql
```

For WAL-based restore, follow your backup tool's procedure. After restore, restart the Cooker pod — the orphan sweep will mark any in-flight runs as failed (since they didn't finish before the snapshot).

> **CRITICAL.** Backing up the database is NOT enough if you use the `database` secrets backend. You also need `COOKER_SECRET_KEY` — it's what decrypts the sealed values in the database. Store the key in your password manager / KMS / sealed-secrets system, NOT in the same backup as the database.

## Retention

`pipeline_runs.stage_runs` JSONB can grow significantly for high-traffic pipelines. The Helm chart ships a retention CronJob at `deploy/helm/cooker/templates/cronjob-retention.yaml`. Enable + configure:

```yaml
# values.yaml
retention:
  enabled: true
  schedule: "0 3 * * *"     # daily at 03:00 UTC
  daysToKeep: 90            # delete runs finished more than 90 days ago
```

The CronJob runs a `DELETE FROM pipeline_runs WHERE finished_at < NOW() - INTERVAL '<days> days'` against the same `DATABASE_URL`. Audit log file rotation is a separate concern (see [Observability](observability.md#audit-log)).

## Connection sizing

Cooker uses `database/sql` connection pooling. The pool size is **not** currently a config knob (it inherits the driver defaults — ~24 in-use, ~8 idle). For a busy install with many concurrent WebSocket clients, you may need:

```text
DATABASE_URL=postgres://...?pool_max_conns=50
```

Confirm against your Postgres `max_connections` setting; running out of connections causes 500s during high-traffic windows.

> **TODO: verify** the exact default pool sizes and whether `pool_max_conns` is honoured by Cooker's chosen driver (`lib/pq`). <!-- TODO: verify -->

## Schema inspection

Cooker's tables, as defined in `001_initial.up.sql` and the subsequent migrations, are normal Postgres tables. Connect with `psql` and `\dt`:

```text
                List of relations
 Schema |       Name        | Type  | Owner
--------+-------------------+-------+--------
 public | apps              | table | cooker
 public | cluster_configs   | table | cooker
 public | environments      | table | cooker
 public | hosts             | table | cooker
 public | pipeline_runs     | table | cooker
 public | pipelines         | table | cooker
 public | registry_configs  | table | cooker
 public | schema_migrations | table | cooker
 public | users             | table | cooker     (only if local auth is enabled)
```

> **TODO: verify** the exact list — there may be additional tables (audit, idempotency, etc.) added in later migrations. <!-- TODO: verify -->

Almost all "data" lives in JSONB columns: `pipelines.stages`, `pipelines.edges`, `pipeline_runs.stage_runs`, etc. Querying inside them uses Postgres's `->`, `->>`, and `jsonb_path_*` operators.

## Common operational issues

- **Pool exhaustion** during a build storm. Symptom: spike in 500s, slow API. Fix: increase `pool_max_conns` and Postgres `max_connections` in lockstep.
- **JSONB query slowness** as run history grows. Symptom: `GET /api/v1/pipelines/:id/runs` takes seconds. Fix: enable retention; for very large installs, add a GIN index on `pipeline_runs.stage_runs` jsonb_path_ops.
- **Advisory lock held forever** if a previous Cooker pod crashed mid-migration with a held lock. Symptom: subsequent boots hang on "applying migrations." Fix: connect with `psql`, run `SELECT pg_advisory_unlock(847263)`, retry.

## Cross-references

- **[Self-hosting tips](../guides/self-hosting-tips.md)** — sizing guidance.
- **[Troubleshooting: pg-migration-errors](../troubleshooting/pg-migration-errors.md)** — symptom-driven.
- **[`docs/RUNBOOK.md`](../../guides/RUNBOOK.md)** — full incident response playbook.
