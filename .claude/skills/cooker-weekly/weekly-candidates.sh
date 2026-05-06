#!/usr/bin/env bash
# Print this week's candidate items for the cooker-weekly routine,
# in the priority order the SKILL.md spells out:
#
#   1. Open issues tagged bug/security
#   2. Open chains in chain-recheck.md
#   3. New chains the remediation pass introduced
#   4. Quick wins (Low/Medium with no blockers)
#
# Output is a flat list of one candidate per line, prefixed with a
# priority bucket [P1] / [P2] / [P3] / [P4]. The skill picks the
# first one it can act on within a single PR.
#
# Lookback window: from the most recent T-weekly-* tag, falling
# back to 8 days if no tag exists.

set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

last_tag="$(git tag --list 'T-weekly-*' --sort=-creatordate | head -1 || true)"
since_arg=()
if [ -n "$last_tag" ]; then
  since_arg=(--since="$(git log -1 --format=%cI "$last_tag")")
else
  since_arg=(--since='8 days ago')
fi

echo "## Lookback window"
if [ -n "$last_tag" ]; then
  echo "since tag $last_tag ($(git log -1 --format=%cI "$last_tag"))"
else
  echo "since 8 days ago (no T-weekly-* tag yet)"
fi
echo

# ---- P1: GitHub issues with bug/security label -----------------
# The skill calls the GitHub MCP tool to list these; this script
# falls back to `gh issue list` when run from a shell, and prints
# nothing (with a stderr note) when neither is available. The
# weekly skill should NOT block on this script's failure here —
# the issue lookup is delegated to the agent.
echo "## [P1] Recent issues tagged bug or security"
if command -v gh >/dev/null 2>&1; then
  gh issue list \
    --state open \
    --label 'bug,security' \
    --json number,title,labels,updatedAt \
    --jq '.[] | "[P1] #\(.number) \(.title) (\([.labels[].name] | join(",")))"' \
    2>/dev/null || echo "  (gh not authenticated; run from the agent via mcp__github__list_issues)"
else
  echo "  (gh CLI not installed; the agent calls mcp__github__list_issues directly)"
fi
echo

# ---- P2: Open chains in chain-recheck.md -----------------------
# The chain-recheck doc uses tables with **Open** in the verdict
# column. Pull the row label by walking back to the nearest "B.X"
# section header so each candidate is identified with its full
# audit ID (B.1.1 etc.).
echo "## [P2] Currently-Open chains (from docs/audits/chain-recheck.md)"
awk '
  /^## B\./ { section = $0; next }
  /^### / { section = $0; next }
  /\*\*Open\*\*/ {
    # The first column of the row is the chain ID and short title.
    # Strip the markdown table pipes.
    gsub(/^\| */, "", $0)
    gsub(/ *\|.*$/, "", $0)
    print "[P2] " $0
  }
' docs/audits/chain-recheck.md
echo

# ---- P3: New chains the remediation introduced -----------------
echo "## [P3] New chains introduced by remediation"
awk '
  /^## New chains introduced/ { in_block = 1; next }
  in_block && /^---|^## / { in_block = 0 }
  in_block && /^[0-9]+\./ {
    line = $0
    gsub(/^[0-9]+\. */, "", line)
    print "[P3] " line
  }
' docs/audits/chain-recheck.md | head -10
echo

# ---- P4: Quick wins from remediation-plan.md -------------------
echo "## [P4] Quick-win themes still listed as pending in remediation-plan.md"
# Anything in the "Phase 4" section (T20-T24) that's tagged
# "[Low]" or "[Medium]" and isn't already marked closed in the
# audit-doc header. The script keeps it heuristic; the skill
# does the final selection.
awk '
  /^## Phase 4/ { in_phase = 1 }
  /^## Phase / && !/^## Phase 4/ { in_phase = 0 }
  in_phase && /^### T[0-9]+/ {
    title = $0
    gsub(/^### */, "", title)
    print "[P4] " title
  }
' docs/audits/remediation-plan.md
echo

# ---- Recent commits since the cutoff ---------------------------
echo "## Recent commits in the lookback window"
git log "${since_arg[@]}" --oneline | head -20
