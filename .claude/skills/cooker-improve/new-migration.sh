#!/usr/bin/env bash
# Scaffold the next pair of postgres migration files for Cooker.
#
# Usage:
#   .claude/skills/cooker-improve/new-migration.sh <topic-slug>
#
# Picks the next NNN by scanning existing files (max + 1, zero-padded
# to 3 digits), creates an empty .up.sql / .down.sql pair with
# placeholder comments, and prints the paths for the caller to edit.
#
# Naming rule (kept consistent with what the postgres store reader
# expects and what the audit docs cite): NNN_<topic_with_underscores>.

set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: new-migration.sh <topic-slug>" >&2
  echo "       e.g.  new-migration.sh runs_partitioning" >&2
  exit 2
fi

topic="$1"
# Replace anything not in [a-zA-Z0-9_] with underscore so the file
# name stays embed-safe.
slug="$(echo "$topic" | tr -c 'a-zA-Z0-9_' '_' | sed 's/__*/_/g; s/^_//; s/_$//')"
if [ -z "$slug" ]; then
  echo "error: empty slug after sanitisation" >&2
  exit 2
fi

root="$(git rev-parse --show-toplevel)"
mig="$root/backend/internal/store/postgres/migrations"

# Find the highest existing NNN among *.up.sql files; fall back to 0
# when the directory is empty (it shouldn't be, but be defensive).
last="$(ls "$mig"/*.up.sql 2>/dev/null \
        | sed -E 's|.*/([0-9]+)_.*|\1|' \
        | sort -n | tail -1)"
last="${last:-000}"
# Strip leading zeros via arithmetic (printf re-pads).
next=$((10#$last + 1))
nnn="$(printf '%03d' "$next")"

up="$mig/${nnn}_${slug}.up.sql"
down="$mig/${nnn}_${slug}.down.sql"

if [ -e "$up" ] || [ -e "$down" ]; then
  echo "error: $up or $down already exists" >&2
  exit 1
fi

cat >"$up" <<EOF
-- ${nnn}_${slug}.up.sql
--
-- TODO: forward migration.
--
-- Convention reminders:
--   * One logical change per migration (don't bundle).
--   * Idempotent where reasonable: CREATE TABLE IF NOT EXISTS,
--     CREATE INDEX IF NOT EXISTS, ALTER TABLE ... ADD COLUMN
--     IF NOT EXISTS.
--   * Keep statements transaction-safe; the runner wraps the
--     whole file in a single tx and commits with the
--     schema_migrations row insert (T15 contract).
--   * NOT NULL columns added on existing tables MUST have a
--     DEFAULT value (otherwise replicas on the old code can't
--     INSERT during the rolling deploy window).

EOF

cat >"$down" <<EOF
-- ${nnn}_${slug}.down.sql
--
-- TODO: reverse the up migration. Must leave the schema in the
-- exact shape it was in before ${nnn}_${slug}.up.sql ran.
--
-- Convention reminders:
--   * Mirror the up's drops in reverse order (objects with FK
--     dependencies first).
--   * Use IF EXISTS variants so re-running on a partially-rolled-
--     back DB is safe.

EOF

echo "$up"
echo "$down"
