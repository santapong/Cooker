#!/usr/bin/env bash
# PreToolUse guard — a belt-and-suspenders backstop to the deny permission rules.
# Blocks (exit 2) any tool call that touches secrets or runs a destructive command,
# feeding the reason back to Claude. Wire in .claude/settings.json:
#   "hooks": { "PreToolUse": [ { "matcher": "Bash|Edit|Write|Read",
#     "hooks": [ { "type": "command",
#       "command": "${CLAUDE_PROJECT_DIR}/.claude/hooks/guard-secrets.sh" } ] } ] }
# Make executable: chmod +x .claude/hooks/guard-secrets.sh
# Test: echo '{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}' | .claude/hooks/guard-secrets.sh; echo $?

set -euo pipefail
INPUT=$(cat)   # PreToolUse event JSON on stdin

tool=$(jq -r '.tool_name // ""' <<<"$INPUT")
cmd=$(jq -r '.tool_input.command // ""' <<<"$INPUT")
path=$(jq -r '.tool_input.file_path // ""' <<<"$INPUT")

block() { echo "Blocked by guard-secrets hook: $1" >&2; exit 2; }

# EDIT ME: tune the patterns to your project's sensitive paths and dangerous commands.
SECRET_RE='(^|/)\.env|(^|/)\.ssh/|(^|/)\.aws/|(^|/)secrets/|id_rsa|\.pem$'
DANGER_RE='rm[[:space:]]+-rf|sudo[[:space:]]|:\(\)\{|mkfs|dd[[:space:]]+if='

if [[ -n "$path" && "$path" =~ $SECRET_RE ]]; then
  block "path '$path' looks like a secret/credential file"
fi

if [[ "$tool" == "Bash" && -n "$cmd" ]]; then
  if [[ "$cmd" =~ $DANGER_RE ]]; then
    block "command matches a destructive pattern: $cmd"
  fi
  if [[ "$cmd" =~ $SECRET_RE ]]; then
    block "command references a secret/credential path: $cmd"
  fi
fi

exit 0   # allow everything else to proceed
