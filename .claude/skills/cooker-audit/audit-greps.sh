#!/usr/bin/env bash
# Run the canonical anti-pattern greps from the cooker-audit skill.
# Each section is a pattern that produced real findings during the
# four-audit + W-series remediation pass. Output is grouped by
# pattern with a one-line description so the reader can prioritise.
#
# Usage:
#   .claude/skills/cooker-audit/audit-greps.sh
#   .claude/skills/cooker-audit/audit-greps.sh <pattern-num>   # only run pattern N
#
# Patterns:
#   1  comment promises behaviour code doesn't do
#   2  per-process in-memory state
#   3  fail-stop on first stage error
#   4  no per-call timeout on external I/O
#   5  dev defaults reaching production
#   6  stub handlers that silently succeed
#   7  missing optimistic concurrency
#   8  cleanup races on row delete
#   9  shell-string interpolation
#  10  unbounded resource growth

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

only="${1:-}"

run() {
  local n="$1" desc="$2"
  shift 2
  if [ -n "$only" ] && [ "$only" != "$n" ]; then
    return 0
  fi
  echo "================================================================"
  echo "Pattern $n: $desc"
  echo "================================================================"
  # Each remaining arg is a separate `rg` invocation; print all hits,
  # tolerate empty matches.
  for cmd in "$@"; do
    eval "$cmd" || true
  done
  echo
}

# 1. Comment promises behaviour the code doesn't do
run 1 "Comment promises behaviour the code doesn't do (highest-yield)" \
  "rg -n -B1 -A6 'extended with a [0-9]+|matches the existing|will retry|cleans up' backend --type go" \
  "rg -n 'TODO|FIXME|XXX' backend --type go | head -20"

# 2. Per-process in-memory state with multi-replica caveat
run 2 "Per-process in-memory state (look for missing Redis backend)" \
  "rg -n 'make\\(map\\[' backend/internal/server" \
  "rg -n 'sync\\.Map' backend/internal/server backend/internal/idempotency backend/internal/observability"

# 3. Fail-stop on first error in a loop over external calls
run 3 "Fail-stop loops over external calls (no retry / no continue-on-error)" \
  "rg -n -B1 -A4 'for [^{]*\\{$' backend/internal/service backend/internal/handler" \
  "rg -n 'return [a-z]+Err$' backend/internal/service"

# 4. No per-call timeout on external I/O
run 4 "External I/O calls — confirm caller wraps in WithTimeout" \
  "rg -n 'PushContext|client\\.Solve|\\.Pods\\([^)]*\\)\\.Get|Jobs\\([^)]*\\)\\.Create|http\\.Client\\{Timeout' backend --type go"

# 5. Dev defaults reaching production
run 5 "Env-var defaults — verify Validate() rejects each in production" \
  "rg -n 'getEnv\\(\"[A-Z_]+\", *\"' backend/internal/config" \
  "rg -n 'os\\.Getenv\\(\"[A-Z_]+\"\\)' backend"

# 6. Stub handlers that silently succeed
run 6 "Stub handlers (slog + return nil; check whether documented)" \
  "rg -n -B2 -A4 '^\\s*slog\\.Info' backend/internal/service backend/internal/handler | grep -B5 'return nil' | head -40"

# 7. Missing optimistic concurrency
run 7 "UPDATEs that don't check the version column" \
  "rg -n 'UPDATE [a-z_]+ SET' backend/internal/store/postgres | rg -v 'version=\\\$|version = \\\$|heartbeat'"

# 8. Cleanup races on row delete
run 8 "Delete handlers — check whether anything in flight is verified first" \
  "rg -n 'func \\(h \\*Handler\\) Delete' backend/internal/handler"

# 9. Shell-string interpolation
run 9 "fmt.Sprintf with embedded user values fed to /bin/sh -c" \
  "rg -n 'fmt\\.Sprintf\\([^)]*-[a-z]' backend/internal/builder backend/internal/gitops backend/internal/source" \
  "rg -n -B2 -A2 'Command:.*sh.*-c' backend/internal/builder backend/internal/handler"

# 10. Unbounded resource growth
run 10 "Reads / appends without size caps" \
  "rg -n 'io\\.ReadAll' backend --type go" \
  "rg -n 'make\\(chan [^,]+, [0-9]+\\)' backend --type go" \
  "rg -n '\\.LimitReader' backend --type go"

if [ -z "$only" ]; then
  echo "----------------------------------------------------------------"
  echo "Done. Pipe each pattern through your reading workflow:"
  echo "  1. Read the cited file (don't trust grep alone)."
  echo "  2. Check the false-positives list in SKILL.md."
  echo "  3. Cross-reference docs/audits/chain-recheck.md."
fi
