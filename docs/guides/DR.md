# Disaster recovery runbook

How to recover Cooker after losing the database, the cluster, or both. This
is the operational companion to the RUNBOOK's [Backup, retention,
restore](./RUNBOOK.md#backup-retention-restore) section: the RUNBOOK
documents the *recipe*; this document states the **targets**, the **full
restore procedure**, and the **two things that are NOT in the database
backup** (the secret key and the build context) — either of which, missed,
turns a clean restore into permanent data loss.

> SRE-readiness grounding: this closes BK-1 ("rehearse the restore drill +
> state RPO/RTO") and KEY-1 ("back up `COOKER_SECRET_KEY` separately") from
> [`docs/launch/00-sre-sla-readiness.md`](../launch/00-sre-sla-readiness.md)
> §3.2 / §5.

---

## Recovery objectives (RPO / RTO)

| Tier | RPO (max data loss) | RTO (max downtime) | Backup mechanism |
|---|---|---|---|
| **Self-hosted (best-effort)** | **≤ 24h** | **≤ 1h** | Nightly `pg_dump --format=custom` to off-site storage |
| Hosted-SaaS v1 (contractual) | ≤ 5 min | ≤ 30 min | Managed Postgres with WAL / point-in-time recovery |

The self-hosted targets above are the supported, drillable shape today. The
hosted-SaaS column is the design target and requires managed Postgres PITR
(RDS / Cloud SQL / Neon / Supabase) — it is not achievable with `pg_dump`
alone.

**If a restore takes longer than the RTO, your backup format is wrong** —
use `pg_dump --format=custom` (parallelisable, selective `pg_restore`), never
`--format=plain`.

---

## What is — and is NOT — covered by a database backup

Postgres is Cooker's only stateful component (pipelines, runs, environments,
apps, hosts, users, `schema_migrations`, and the JSONB run history on
`pipeline_runs`). A Postgres backup recovers **all of that**, and the schema
itself is re-creatable from an empty database via the embedded migrations.

Two things are deliberately **outside** the database and must be recovered
independently. Missing either is a silent, unrecoverable failure:

### 1. `COOKER_SECRET_KEY` — escrow it SEPARATELY from the DB

Environment secrets are sealed with AES-GCM under `COOKER_SECRET_KEY` and
stored encrypted in Postgres. **If the key is lost, every sealed secret is
permanently unrecoverable** — restoring the database gives you ciphertext you
can never open. Dual-key rotation (roadmap R1 / T19) does not exist today;
today, **key loss = secrets loss**.

Therefore:

- **Escrow `COOKER_SECRET_KEY` in a different blast radius from the DB
  backup.** A backup that bundles the key next to the ciphertext defeats the
  point — one compromised bucket loses both.
- The AWS IaC **already escrows the key in AWS Secrets Manager**, separate
  from the database. For other platforms, mirror that: a dedicated secrets
  manager (Vault, GCP/Azure secret store) or sealed offline backup, never the
  same bucket as the `pg_dump` artifacts.
- Treat key loss as a **data-loss incident** — engage security/compliance in
  parallel with L2 (see the RUNBOOK escalation ladder).

### 2. The EFS build context is NOT backed up

The shared build-context volume (EFS in the AWS deployment) holds transient
build inputs/workspaces. **It is intentionally not part of the backup plan**
— build contexts are ephemeral and reproducible from source. After a restore,
in-flight builds whose context lived only on that volume cannot be resumed;
the orphan sweep marks their runs failed on next boot, and they are simply
re-triggered. Do not size your RTO/RPO around recovering build contexts; they
are throwaway by design.

### 3. KeepSave-as-source-of-truth

If `COOKER_SECRETS_BACKEND=keepsave`, secrets live in KeepSave, not in
Cooker's Postgres — there is **no DB fallback**. KeepSave's own backup is then
the only copy, and its DR story must be included in your plan alongside
Postgres. Restoring Cooker's DB does **not** restore those secrets.

---

## Backup options (pick one)

The Helm chart does **not** ship a backup operator — the operator chooses and
wires one of:

- **Managed Postgres PITR** (RDS / Cloud SQL / Aiven / Neon / Supabase) —
  turn on WAL archiving / point-in-time recovery. The only path to the
  hosted-SaaS ≤ 5 min RPO. **Recommended for production.**
- **Velero** with the `csi-snapshotter` plugin — block-level snapshot of the
  Postgres PVC. Good when Postgres runs in-cluster on a CSI-backed volume.
- **Bitnami `postgresql` chart with `backup.enabled=true`** — `pg_basebackup`
  to S3-compatible storage. Simplest when Postgres is co-installed.
- **`pg_dump` cron (self-hosted minimum)** — a daily
  `pg_dump --format=custom > /backups/cooker-$(date +%F).dump` with 30-day
  retention, shipped **off-site** (never the same node as the database). This
  is the mechanism the [drill](#rehearsing-the-drill) and the
  [restore procedure](#restore-procedure) below exercise.

Whatever you pick: **store backups off the node that runs the database**, and
escrow `COOKER_SECRET_KEY` somewhere else again.

---

## Restore procedure

Run this against the target cluster/namespace. Budget: under the RTO above.

1. **Quiesce the app** so no writes hit the database mid-restore:
   ```sh
   kubectl scale deployment/cooker --replicas=0 -n <namespace>
   ```
2. **Restore the database** from the most recent good backup:
   ```sh
   pg_restore --clean --if-exists --no-owner -d "$DATABASE_URL" \
     /backups/cooker-YYYY-MM-DD.dump
   ```
   (For managed PITR, use the provider's point-in-time restore to just before
   the incident instead of this step.)
3. **Recover `COOKER_SECRET_KEY`** from its separate escrow (see above). If
   the key is genuinely lost, run `cooker generate-key` — but understand that
   all previously sealed environment secrets are gone and must be re-entered.
   Ensure the Deployment's `secretKeyRef` points at the recovered key.
4. **Scale back up and verify readiness**:
   ```sh
   kubectl scale deployment/cooker --replicas=N -n <namespace>
   kubectl rollout status deployment/cooker -n <namespace>
   ```
   Then confirm the platform is healthy:
   - `GET /health/ready` returns **200** (DB ping + Redis ping + JWKS age all
     pass).
   - `GET /api/v1/version` reflects the expected running build.
5. **Smoke-test the data path**: list pipelines, fetch one historical run,
   and trigger a no-op (`custom`-stage) pipeline to confirm orchestration end
   to end.

---

## Rehearsing the drill

Backups you have never restored are not backups. Rehearse **at least
quarterly** (and once as a pre-launch game day). The repo ships a self-
contained, non-destructive drill that proves the dump → restore path without
touching the source database:

```sh
make backup-restore-drill
# or against an explicit DSN:
DATABASE_URL=postgres://user:pass@host:5432/cooker make backup-restore-drill
```

It runs `pg_dump --format=custom` of the source DB (read-only), restores into
a throwaway scratch database, reports per-table row counts, and tears the
scratch DB down — printing **PASS** or **FAIL**. See
[`scripts/backup-restore-drill.sh`](../../scripts/backup-restore-drill.sh)
for the configuration knobs.

**The drill validates Postgres only.** It cannot validate
`COOKER_SECRET_KEY` recovery — that is an out-of-band escrow check you must
perform separately (confirm the key in your secrets manager decrypts a known
sealed secret). A green drill plus a verified key escrow is the complete
self-hosted DR proof.
