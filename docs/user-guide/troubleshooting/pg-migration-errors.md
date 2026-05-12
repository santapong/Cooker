# Postgres migration errors

Cooker applies embedded migrations at boot. Things that can go wrong: held advisory lock, partial application, version mismatch, connection failures.

## 1. Boot hangs on "applying migrations"

**Symptom:** Cooker logs `{"level":"INFO","msg":"store: applying migrations"}` and never proceeds.

**Cause:** A previous Cooker pod crashed while holding `pg_advisory_lock(847263)`. The lock survives the dead session.

**Check:**

```sql
psql "$DATABASE_URL"
SELECT pid, locktype, mode, granted
FROM pg_locks
WHERE locktype = 'advisory' AND objid = 847263;
```

If you see a row with a `pid` that no longer exists (`SELECT pid FROM pg_stat_activity WHERE pid = <held>;` returns empty), the lock is stale.

**Fix:**

```sql
SELECT pg_advisory_unlock(847263);
-- or, if not in the same session:
SELECT pg_advisory_unlock_all();  -- nukes everything (caution)
```

Restart the Cooker pod. The next boot will acquire the lock cleanly.

## 2. Migration failed mid-apply

**Symptom:** Cooker logs `{"level":"ERROR","msg":"store: migration failed","version":N,"err":"..."}` and the pod CrashLoopBackOff's.

**Cause:** A SQL statement in `00N_xxx.up.sql` failed. Could be:

- Schema already partially exists from a botched prior run.
- A column type change that the data doesn't support.
- A constraint violation.

Migrations are run inside transactions, so a failed migration leaves the schema in the pre-migration state. But the **migration version table** isn't updated, so the next boot tries the same migration again.

**Check:**

```sql
-- What version is currently applied?
SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;
```

If it's `N-1` and the boot is trying `N`, the migration genuinely hasn't applied yet.

**Fix:**

- Read the error. Fix the underlying cause (e.g. a duplicate row blocking a `UNIQUE` add).
- If the migration genuinely can't be applied, you need to either:
  - Roll Cooker back to an older binary (one that doesn't ship this migration), OR
  - Hand-apply a patched migration. **This is delicate; test on a copy of the DB first.**

> **Operator support gap.** There's no `cooker migrate down N` CLI yet (`shipping-go 30-90d` #13). The `.down.sql` files in `backend/internal/store/postgres/migrations/` are your hand-run reference.

## 3. Connection refused / connection reset

**Symptom:** Cooker logs `failed to open postgres: dial tcp ...: connection refused`.

**Cause:** Postgres unreachable. Either it's down, the URL is wrong, or the network blocks it.

**Check:**

```sh
# From inside the Cooker pod:
kubectl -n cooker exec deploy/cooker -- nc -zv <postgres-host> 5432
```

Look at:

- Is the Postgres service / pod running?
- Is the URL correct (host, port, db name, username)?
- Are credentials right? Test outside Cooker:
  ```sh
  psql "$DATABASE_URL"
  ```

**Fix:** Address whatever the connection test reveals.

## 4. SSL required but not provided

**Symptom:** Boot logs `pq: SSL is not enabled on the server` or `pq: SSL connection is required`.

**Cause:** Mismatch between Cooker's `sslmode` and Postgres's `ssl` setting:

- `sslmode=require` against a Postgres without SSL → fails.
- `sslmode=disable` against a Postgres that requires SSL → fails.

**Check:**

```sql
SHOW ssl;     -- on the Postgres instance
```

Compare against the `?sslmode=` query param in your `DATABASE_URL`.

**Fix:** Either enable SSL on Postgres (recommended for production) and use `sslmode=require` (or stronger), or accept the security implications of plaintext.

> **Production warning.** `Config.Validate()` does NOT currently reject `?sslmode=disable` in production (`S26-05-10`). Set this correctly by hand.

## 5. Schema-version vs binary-version mismatch

**Symptom:** Boot proceeds, but features behave strangely. New columns are NULL where you expect values; some endpoints 404.

**Cause:** You're running an OLDER Cooker binary against a Postgres that was migrated by a NEWER one. The old binary's migration code sees that `schema_migrations.version` is ahead of what it knows; it does nothing and starts. The new columns / tables are present but unused.

**Check:**

```sql
SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;
```

Compare against what your binary version expects (you'd need to read the source until `cooker version` ships):

```sh
ls backend/internal/store/postgres/migrations/
# 001_initial.up.sql
# 002_*.up.sql
# ...
# The highest-numbered file is the "expected" version.
```

**Fix:** Run a binary that matches the schema. The roadmap fix (refuse to start on version mismatch) is `shipping-go 30-90d` #4.

## 6. Disk full

**Symptom:** Postgres returns `ERROR: could not extend file ...: No space left on device` mid-migration.

**Cause:** The Postgres data volume is full.

**Check:**

```sh
kubectl -n <pg-namespace> exec <pg-pod> -- df -h
```

**Fix:** Resize the PVC, OR delete old data (e.g. enable retention; see [Postgres: retention](../operations/postgres.md#retention)). Then re-run the migration.

## 7. Migration would lose data

**Symptom:** A migration that drops a column or `ALTER TABLE` with `NOT NULL` fails because existing rows would violate it.

**Cause:** The migration assumes the data is in a state your install doesn't have.

**Check:** Read the migration's SQL in `backend/internal/store/postgres/migrations/`. Compare against your actual data.

**Fix:** This requires a human decision — clean up the data first, then re-run the migration. Take a backup before doing anything.

```sh
pg_dump "$DATABASE_URL" > before-fixup.sql
```

## Last resort — recover from backup

If a migration leaves the DB in an unrecoverable state and you have a backup from before the upgrade:

```sh
# Drop the broken DB and recreate.
psql "$DATABASE_URL" -c "DROP DATABASE cooker; CREATE DATABASE cooker;"

# Restore the backup.
psql "$DATABASE_URL" < before-fixup.sql

# Boot the OLD binary (the one that was working).
```

> **Pre-flight reminder.** Always take a backup before upgrading. See [Upgrading: pre-flight checklist](../getting-started/upgrading.md#pre-flight-checklist).

## Cross-references

- **[Postgres](../operations/postgres.md)** — migration semantics in detail.
- **[Upgrading](../getting-started/upgrading.md)** — pre-flight checklist.
- **[`docs/RUNBOOK.md`](../../RUNBOOK.md)** — incident response.
