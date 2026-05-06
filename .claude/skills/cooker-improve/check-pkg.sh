#!/usr/bin/env bash
# Scoped pre-commit check for Cooker.
#
# Usage:  .claude/skills/cooker-improve/check-pkg.sh <pkg-rel-to-backend>
# Example: .claude/skills/cooker-improve/check-pkg.sh internal/handler
#
# Runs the same three signals CI does, but scoped to one package
# subtree so the inner loop is fast:
#   1. gofmt drift detector (over the whole backend; cheap)
#   2. go vet on the target subtree
#   3. go test -race on the target subtree
#
# Exits 1 on the first failure so the caller can fix and re-run.

set -euo pipefail

pkg="${1:-./...}"

# Allow running from anywhere in the worktree.
root="$(git rev-parse --show-toplevel)"
cd "$root/backend"

# Normalise: accept "internal/handler", "./internal/handler",
# "backend/internal/handler", or "./...".
pkg="${pkg#backend/}"
pkg="${pkg#./}"
if [ "$pkg" != "..." ] && [ ! -d "$pkg" ]; then
  echo "error: package $pkg not found under backend/" >&2
  exit 2
fi

echo "==> gofmt"
drift="$(gofmt -l . 2>/dev/null || true)"
if [ -n "$drift" ]; then
  echo "  gofmt drift:" >&2
  echo "$drift" | sed 's/^/    /' >&2
  echo "  fix with: (cd backend && gofmt -w $drift)" >&2
  exit 1
fi

echo "==> go vet ./$pkg/..."
go vet "./$pkg/..."

echo "==> go test -race -timeout 90s ./$pkg/..."
go test -race -timeout 90s "./$pkg/..."

echo "OK"
