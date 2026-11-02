# Disposable backup and restore drill

`TestPostgresBackupRestoreLive` exercises an actual PostgreSQL logical backup
and restore using only generated fixture data. It creates one uniquely named
PostgreSQL container, binds it to a random loopback port, and uses two separate
databases. It never accepts an existing database URL or reads a configured
Cooker encryption key.

With Go 1.25 and Docker available, run from `backend/`:

```sh
COOKER_RESTORE_ACCEPTANCE=true go test -tags integration ./internal/service \
  -run '^TestPostgresBackupRestoreLive$' -count=1 -v -timeout=5m
```

The fixture uses `postgres:16-alpine` and matching `pg_dump`/`pg_restore` tools
inside its own container. Set `COOKER_RESTORE_EVIDENCE_FILE` to a new local file
to retain a sanitized JSON report. Without the opt-in flag, the test skips.

The drill:

1. Applies all current Cooker migrations to the source database, then creates
   an encrypted environment probe, encrypted Git source token/webhook probe,
   an App/deployment graph, a run with its actor, deployment history, and a
   durable job that requires reconciliation. All credentials are fake. A random
   encryption key exists only in test-process memory.
2. Persists a log with the probe masked before storage. Takes a custom-format
   `pg_dump` while fixture writers are quiescent, creates an empty target
   database, and restores with `--single-transaction --exit-on-error`.
3. Verifies the migration ledger before reopening Cooker stores, then verifies
   reopening does not change it. The original encryption key reads both probe
   types. Configuration, record versions and ciphertext match the source.
4. Tries another random key. Environment and source reads fail; preserving a
   source connection's credentials through an update also fails. Stored
   ciphertext and record versions remain unchanged, and the original key still
   works. This does not attempt to replace a secret with a newly supplied value.
5. Checks the restored job state, concurrency key, canonical payload digest,
   actor, run and deployment history. Redacted log contents and replay cursors
   match; a new entry continues at the next cursor.
6. Deletes the temporary archive and the exact owned container plus volume,
   and verifies no owned resource remains. No encryption key is printed or
   retained with the report.

## Observed result — 28 September 2026, 20:52 ICT

The drill passed in **3.00 seconds** with PostgreSQL/pg_dump 16.13. All 31
migrations (`001_initial` through `031_source_connections`) survived restore,
the same-key and wrong-key checks passed, durable evidence survived, and cleanup
was verified. The custom archive was 61,717 bytes and was removed after the
test; its hash is recorded for the executed artifact.

Local evidence: `/mnt/data/cooker-restore-drill-20260928/acceptance.json`,
`run.log`, `compile.log`, the frozen test source, and `source-sha256.json`.

This proves the tested same-version logical restore of quiescent fixture data.
It does not establish a production upgrade from an older schema, live-backup
coordination with external deployments, online key rotation, role/ACL recovery,
point-in-time recovery, recovery time at production scale, or recovery without
the original key. Keep the actual encryption-key backup separate from database
backups; this fixture never exercises a real installation's key.
