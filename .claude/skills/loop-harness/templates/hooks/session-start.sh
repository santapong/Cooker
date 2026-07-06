#!/usr/bin/env bash
# SessionStart hook — orient Claude at the start of every session.
# Wire in .claude/settings.json:
#   "hooks": { "SessionStart": [ { "matcher": "startup",
#     "hooks": [ { "type": "command",
#       "command": "${CLAUDE_PROJECT_DIR}/.claude/hooks/session-start.sh" } ] } ] }
# Make executable: chmod +x .claude/hooks/session-start.sh
#
# It prints JSON on stdout with additionalContext, which Claude Code injects
# into the session's context. Keep it fast and side-effect-free.

set -euo pipefail
INPUT=$(cat)   # SessionStart event JSON on stdin (has .source: startup|resume|clear|compact)

branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
recent=$(git log --oneline -3 2>/dev/null || echo "(no git history)")

# EDIT ME: project-specific reminders the model should start each session with.
read -r -d '' CONTEXT <<EOF || true
Project harness reminders:
- Current branch: ${branch}
- Recent commits:
${recent}
- Run tests before committing; never force-push to main.
EOF

# Emit as SessionStart additionalContext (JSON escaped via jq).
jq -n --arg ctx "$CONTEXT" \
  '{hookSpecificOutput: {hookEventName: "SessionStart", additionalContext: $ctx}}'

exit 0
