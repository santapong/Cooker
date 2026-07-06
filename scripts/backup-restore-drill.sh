#!/usr/bin/env bash
# scripts/backup-restore-drill.sh — exercise Cooker's PostgreSQL
# backup → restore path end-to-end and report PASS/FAIL.
#
# This makes the RUNBOOK "Backup, retention, restore" drill and the
# DR runbook (docs/guides/DR.md) *runnable* rather than just documented
# (SRE readiness gap BK-1 / launch-readiness §6 unchecked box).
#
# Flow:
#   1. pg_dump --format=custom the SOURCE database to a temp .dump file.
#   2. CREATE a scratch database alongside it (dropped on exit).
#   3. pg_restore --clean --if-exists --no-owner the dump into scratch.
#   4. Sanity-check: list the restored tables and report per-table row
#      counts; assert the dump restored at least one table.
#   5. Tear down the scratch database and temp dump (always, via trap).
#
# It is read-only with respect to the SOURCE database (pg_dump only) —
# the restore happens into a throwaway scratch DB so a real production
# DSN can be drilled safely. NOTHING is written to the source.
#
# This intentionally drills ONLY Postgres. COOKER_SECRET_KEY is NOT a
# database concern: AES-GCM-sealed secrets are unrecoverable if the key
# is lost, so the key must be escrowed SEPARATELY from the DB. See
# docs/guides/DR.md ("Secret-key escrow") — this drill cannot and does
# not validate key recovery.
#
# Configuration (env):
#   DATABASE_URL   Full libpq connection URI for the SOURCE database,
#                  e.g. postgres://user:pass@host:5432/cooker?sslmode=require
#                  When set, it takes precedence over the PG* vars below.
#   PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE
#                  Standard libpq env vars, used when DATABASE_URL is unset.
#                  Defaults mirror the UAT/dev stack: localhost:5432,
#                  user "cooker", database "cooker".
#   DRILL_SCRATCH_DB
#                  Name of the scratch restore target (default:
#                  cooker_drill_<epoch>). It is CREATEd and DROPped by
#                  this script; it MUST NOT be a real database.
#   DRILL_KEEP=1   Keep the scratch DB and dump file on exit (debugging).
#
# Requires: pg_dump, pg_restore, psql, createdb, dropdb on PATH
# (PostgreSQL client tools; the server can be remote). The client major
# version should be >= the server's.
#
# Invoked via `make backup-restore-drill`.

set -euo pipefail

log() { printf '[drill %s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required tool: $1 (install the PostgreSQL client tools)" >&2
    exit 2
  }
}

require pg_dump
require pg_restore
require psql
require createdb
require dropdb

# --- Resolve source connection -------------------------------------------
#
# We support two mutually exclusive styles:
#   * DATABASE_URL  → passed via -d to the source-side tools as a URI.
#   * PG* env vars  → libpq picks them up implicitly; we still build a
#                     "maintenance" connection (to the default DB) so we
#                     can CREATE/DROP the scratch database.
#
# For the scratch CREATE/DROP/RESTORE we always connect to a *maintenance*
# database on the same server (never the source DB), because you cannot
# CREATE DATABASE while connected to the DB you are dropping/replacing.

SCRATCH_DB="${DRILL_SCRATCH_DB:-cooker_drill_$(date +%s)}"

# Maintenance DB used only for CREATE/DROP DATABASE statements.
MAINT_DB="${DRILL_MAINT_DB:-postgres}"

dump_file=""
created_scratch=0

cleanup() {
  local rc=$?
  if [[ "${DRILL_KEEP:-0}" == "1" ]]; then
    log "DRILL_KEEP=1 — leaving scratch DB '${SCRATCH_DB}' and dump '${dump_file}' in place"
    return
  fi
  if (( created_scratch == 1 )); then
    log "dropping scratch database '${SCRATCH_DB}'"
    drop_scratch || log "warning: failed to drop scratch DB '${SCRATCH_DB}' — clean it up manually"
  fi
  if [[ -n "${dump_file}" && -f "${dump_file}" ]]; then
    rm -f "${dump_file}"
  fi
  return $rc
}
trap cleanup EXIT INT TERM

if [[ -n "${DATABASE_URL:-}" ]]; then
  # Derive a maintenance URI by swapping the path (database name) for the
  # maintenance DB. Strip any existing path + leading slash, then re-append.
  # This is a string transform on the URI: scheme://[user[:pass]@]host[:port]/<db>[?query]
  base="${DATABASE_URL%%\?*}"           # drop ?query
  query=""
  [[ "${DATABASE_URL}" == *\?* ]] && query="?${DATABASE_URL#*\?}"
  prefix="${base%/*}"                    # everything up to (not incl.) the db name
  MAINT_URI="${prefix}/${MAINT_DB}${query}"
  SCRATCH_URI="${prefix}/${SCRATCH_DB}${query}"

  source_conn=(-d "${DATABASE_URL}")
  maint_conn=(-d "${MAINT_URI}")
  scratch_conn=(-d "${SCRATCH_URI}")
  log "source: DATABASE_URL (database from URI), maintenance DB: ${MAINT_DB}"
else
  : "${PGHOST:=localhost}"
  : "${PGPORT:=5432}"
  : "${PGUSER:=cooker}"
  : "${PGDATABASE:=cooker}"
  export PGHOST PGPORT PGUSER PGDATABASE
  [[ -n "${PGPASSWORD:-}" ]] && export PGPASSWORD

  # Source tools read PG* implicitly (PGDATABASE = source DB).
  source_conn=()
  # Maintenance + scratch override only the dbname; PGHOST/PORT/USER persist.
  maint_conn=(-d "${MAINT_DB}")
  scratch_conn=(-d "${SCRATCH_DB}")
  log "source: PG* env (host=${PGHOST} port=${PGPORT} user=${PGUSER} db=${PGDATABASE}), maintenance DB: ${MAINT_DB}"
fi

drop_scratch() {
  # --if-exists keeps this idempotent; --force terminates lingering
  # connections (PostgreSQL 13+). Fall back without --force on older.
  dropdb "${maint_conn[@]}" --if-exists --force "${SCRATCH_DB}" 2>/dev/null \
    || dropdb "${maint_conn[@]}" --if-exists "${SCRATCH_DB}"
}

# --- 0. Preflight: source must be reachable -------------------------------
log "checking source database connectivity"
if ! psql "${source_conn[@]}" -At -c 'SELECT 1' >/dev/null 2>&1; then
  fail "cannot connect to source database (check DATABASE_URL / PG* env)"
fi

# --- 1. Dump the source (custom format) -----------------------------------
dump_file="$(mktemp -t cooker-drill.XXXXXX.dump)"
log "dumping source → ${dump_file} (pg_dump --format=custom)"
if ! pg_dump "${source_conn[@]}" --format=custom --no-owner --no-acl --file="${dump_file}"; then
  fail "pg_dump of source database failed"
fi
dump_bytes=$(wc -c <"${dump_file}" | tr -d ' ')
log "dump complete: ${dump_bytes} bytes"
(( dump_bytes > 0 )) || fail "pg_dump produced an empty dump file"

# --- 2. Create the scratch database ---------------------------------------
log "creating scratch database '${SCRATCH_DB}'"
# Drop any stale leftover of the same name first (idempotent reruns).
drop_scratch || true
if ! createdb "${maint_conn[@]}" "${SCRATCH_DB}"; then
  fail "could not create scratch database '${SCRATCH_DB}'"
fi
created_scratch=1

# --- 3. Restore into scratch ----------------------------------------------
log "restoring dump into '${SCRATCH_DB}' (pg_restore --clean --if-exists --no-owner)"
# pg_restore exits non-zero on the first hard error; benign "does not
# exist, skipping" notices from --clean --if-exists are warnings, not
# errors. We capture stderr so a genuine failure is reported clearly.
restore_log="$(mktemp -t cooker-drill-restore.XXXXXX.log)"
if ! pg_restore "${scratch_conn[@]}" --clean --if-exists --no-owner --no-acl \
      "${dump_file}" 2>"${restore_log}"; then
  echo "----- pg_restore stderr -----" >&2
  cat "${restore_log}" >&2 || true
  rm -f "${restore_log}"
  fail "pg_restore into scratch database failed"
fi
rm -f "${restore_log}"
log "restore complete"

# --- 4. Sanity check: enumerate tables + row counts -----------------------
log "verifying restored schema"
tables=$(psql "${scratch_conn[@]}" -At -c \
  "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")

if [[ -z "${tables}" ]]; then
  fail "scratch database has no tables in schema 'public' after restore"
fi

table_count=0
echo "----- restored table row counts (scratch: ${SCRATCH_DB}) -----" >&2
while IFS= read -r tbl; do
  [[ -z "${tbl}" ]] && continue
  count=$(psql "${scratch_conn[@]}" -At -c \
    "SELECT count(*) FROM \"${tbl}\"" 2>/dev/null || echo "?")
  printf '  %-32s %s\n' "${tbl}" "${count}" >&2
  table_count=$((table_count + 1))
done <<<"${tables}"
echo "-------------------------------------------------------------" >&2

(( table_count > 0 )) || fail "no tables enumerated after restore"

# Cooker-specific spot check: if schema_migrations exists, the restore
# carried Cooker's own schema (not just an arbitrary DB). Informational —
# absence is not a failure (a fresh/empty source legitimately has none).
if echo "${tables}" | grep -qx 'schema_migrations'; then
  applied=$(psql "${scratch_conn[@]}" -At -c \
    "SELECT count(*) FROM schema_migrations" 2>/dev/null || echo "?")
  log "Cooker schema detected: schema_migrations rows = ${applied}"
fi

log "PASS: dumped source, restored ${table_count} table(s) into scratch DB '${SCRATCH_DB}', verified row counts"
echo "PASS"
