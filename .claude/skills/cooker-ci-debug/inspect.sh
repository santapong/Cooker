#!/usr/bin/env bash
# Print the latest CI status for a Cooker PR in a digestible form,
# plus a one-liner local-repro hint per failed check.
#
# Usage:  inspect.sh <PR-number>
#
# Requires the GitHub MCP tools (the agent invokes them) OR the
# `gh` CLI authenticated against the repo. When neither is
# available, prints a guidance message and exits 0 — the skill
# narrative tells the agent to fall back to mcp__github__pull_request_read.

set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: inspect.sh <PR-number>" >&2
  exit 2
fi

pr="$1"

if ! command -v gh >/dev/null 2>&1; then
  cat <<EOF
gh CLI not available; ask the agent to run:

  mcp__github__pull_request_read
    method: get_check_runs
    owner: santapong
    repo: Cooker
    pullNumber: $pr

and look for check_runs[*].conclusion == "failure".
EOF
  exit 0
fi

# Most-recent run per check name. The JSON is "name", "conclusion",
# "completedAt", "detailsUrl" per check_run.
gh pr checks "$pr" --required=false --json bucket,name,state,workflow,link \
  --jq 'sort_by(.name) | .[]
        | "\(.name)\t\(.bucket)\t\(.state)\t\(.workflow)\t\(.link)"' \
  | column -t -s $'\t'

echo
echo "Failed checks need a per-job repro — see .claude/skills/cooker-ci-debug/SKILL.md"
