# Hooks

Hooks are shell commands Claude Code runs automatically at lifecycle events — to inject context, enforce policy, or react to actions. They turn "remember to do X" into "X happens on its own."

## Structure in settings.json

```json
{
  "hooks": {
    "<EventName>": [
      {
        "matcher": "<pattern>",
        "hooks": [
          { "type": "command", "command": "${CLAUDE_PROJECT_DIR}/.claude/hooks/my-hook.sh" }
        ]
      }
    ]
  }
}
```

Nesting: `hooks` → event name → array of matcher objects → each has a `matcher` and a `hooks` array of `{ "type": "command", "command": ... }`. Optional per-hook fields include `timeout` (seconds) and `statusMessage`.

- **matcher** — for tool events it matches the tool name (`"Bash"`, `"Edit|Write"`, `"*"`); for `SessionStart` it matches the source (`startup`, `resume`, `clear`, `compact`); empty/omitted matches all.
- Reference project files with **`${CLAUDE_PROJECT_DIR}`** and keep scripts under `.claude/hooks/`, marked executable (`chmod +x`).

## The events you'll use most

| Event | Fires | Typical use |
|---|---|---|
| `SessionStart` | session begins/resumes | inject context, run setup, load env |
| `UserPromptSubmit` | user submits a prompt | add context, validate input |
| `PreToolUse` | before a tool runs | guard/deny dangerous actions |
| `PostToolUse` | after a tool succeeds | format, lint, test edited files |
| `Notification` | Claude sends a notification | desktop/OS notify |
| `Stop` | Claude finishes responding | summarize, remind, check state |
| `SubagentStop` | a subagent finishes | aggregate subagent output |
| `PreCompact` | before context compaction | persist state |
| `SessionEnd` | session terminates | cleanup |

(More events exist; these cover almost all harness needs.)

## Input: JSON on stdin

Every hook receives a JSON event on stdin. Common fields:

```json
{
  "session_id": "…",
  "transcript_path": "/…/transcript.jsonl",
  "cwd": "/…",
  "hook_event_name": "PreToolUse"
}
```

Tool events add `tool_name` and `tool_input` (e.g. `tool_input.command` for Bash, `tool_input.file_path` for Edit/Write). `SessionStart` adds `source`; `UserPromptSubmit` adds `prompt`. Read it with `INPUT=$(cat)` then `jq -r '.tool_input.file_path' <<<"$INPUT"`.

## Output: control by exit code

| Exit | Effect |
|---|---|
| `0` | Proceed. If stdout is JSON, its fields apply (see below); otherwise stdout is shown/ignored per event. |
| `2` | **Block.** stderr is fed back to Claude as feedback. Blocks `PreToolUse`, `UserPromptSubmit`, etc. |
| other | Non-blocking error: stderr shown as a warning; the action proceeds. |

On exit `0`, a hook may print JSON to control flow:

- Add context (any event): `{ "additionalContext": "…" }` — injected into Claude's context. For `SessionStart`, use `{ "hookSpecificOutput": { "hookEventName": "SessionStart", "additionalContext": "…" } }`.
- Gate a tool (`PreToolUse`): `{ "hookSpecificOutput": { "hookEventName": "PreToolUse", "permissionDecision": "allow|deny|ask", "permissionDecisionReason": "…" } }`.
- Stop the turn: `{ "continue": false, "stopReason": "…" }`.

## High-value recipes (shipped as templates)

- **SessionStart context/setup** (`templates/hooks/session-start.sh`) — print project reminders, current branch, and recent commits as `additionalContext`; the model starts each session oriented.
- **PreToolUse secret/danger guard** (`templates/hooks/guard-secrets.sh`) — inspect `tool_input`; `exit 2` if a command or file path touches `.env`, `.ssh`, `.aws`, or a destructive `rm -rf`. A belt-and-suspenders backstop to the `deny` permission rules.
- **PostToolUse formatter** — on `Edit|Write`, pipe the changed `file_path` to your formatter: `jq -r '.tool_input.file_path' | xargs npx prettier --write 2>/dev/null || true`. Keep it non-fatal so a missing formatter never blocks work.

## Testing a hook

Pipe it a sample event and check the exit code:

```bash
echo '{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}' | .claude/hooks/guard-secrets.sh; echo "exit=$?"
```
