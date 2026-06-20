#!/usr/bin/env bash
# .agent/rotate.sh — rotate the orchestration event log to keep files small.
# Usage: bash .agent/rotate.sh   (run by the PM before appending a log entry)
# When log/current.md exceeds MAX_LINES, archive it and start fresh.
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG="$DIR/log/current.md"
INDEX="$DIR/log/index.md"
MAX_LINES="${AGENT_LOG_MAX_LINES:-400}"

[ -f "$LOG" ] || { echo "no current.md; nothing to rotate"; exit 0; }

lines=$(wc -l < "$LOG" | tr -d ' ')
if [ "$lines" -le "$MAX_LINES" ]; then
  echo "current.md=$lines lines (cap=$MAX_LINES) — no rotation"
  exit 0
fi

# Next archive number (zero-padded), e.g. archive-001-2026-06-20.md
n=1
while [ -f "$DIR/log/$(printf 'archive-%03d' "$n")"-*.md ]; do n=$((n+1)); done 2>/dev/null || true
# robust count: highest existing index + 1
existing=$(ls "$DIR"/log/archive-*.md 2>/dev/null | sed -E 's/.*archive-([0-9]+)-.*/\1/' | sort -n | tail -1 || true)
[ -n "${existing:-}" ] && n=$((10#$existing + 1)) || n=1

stamp=$(date +%Y-%m-%d)
archive="$DIR/log/$(printf 'archive-%03d-%s.md' "$n" "$stamp")"
mv "$LOG" "$archive"

# Index the archive (first + last timestamped headings as the range)
first=$(grep -m1 -E '^## \[' "$archive" | sed -E 's/^## //' || echo "?")
last=$(grep -E '^## \[' "$archive" | tail -1 | sed -E 's/^## //' || echo "?")
[ -f "$INDEX" ] || printf '# Log archive index\n\n' > "$INDEX"
printf -- '- %s — %s  →  %s\n' "$(basename "$archive")" "$first" "$last" >> "$INDEX"

# Fresh current.md with a back-link
cat > "$LOG" <<EOF
# Cooker Launch — Event Log (current)

> Append-only. Continued from \`$(basename "$archive")\` (see \`index.md\`).
> Snapshot/board lives in \`../state.md\`.

EOF

echo "rotated → $(basename "$archive") ($lines lines); fresh current.md started"
