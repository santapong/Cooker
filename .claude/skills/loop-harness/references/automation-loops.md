# Automation Loops

The "loop engineering" side of the harness: how to make Claude Code do recurring or unattended work without a human re-typing the prompt. Several mechanisms exist; the skill is choosing the *lightest* one that meets the durability need.

## The mechanisms

### SessionStart hook — run setup on every session
Not a loop per se, but the foundation: a `SessionStart` hook (see `hooks.md`) runs a script at the start of every session to load env, inject context, or re-orient after compaction. Use it so every session begins in a known state.

```json
{ "hooks": { "SessionStart": [ { "matcher": "startup",
  "hooks": [ { "type": "command", "command": "${CLAUDE_PROJECT_DIR}/.claude/hooks/session-start.sh" } ] } ] } }
```

### `/loop [interval] [prompt]` — recurring work in an active session
Runs a prompt on a schedule during the current session.

- Fixed interval: `/loop 5m check if the deploy finished` (units: `s`/`m`/`h`/`d`, min ~1 minute).
- Dynamic interval: `/loop check CI status` — Claude picks the cadence (≈1m–1h) from what it observes; usually more efficient than fixed polling.
- Bare `/loop` uses a built-in maintenance prompt, or `.claude/loop.md` if present (see `templates/loop.md`).
- Session-scoped: persists on `--resume`/`--continue`, expires after ~7 days, cancel with Esc while waiting.

### Scheduled tasks (Cron) — periodic checks while a session is open
`CronCreate` / `CronList` / `CronDelete` schedule cron-style tasks (standard 5-field expressions like `*/15 * * * *`). They fire **between turns while the session is idle** — no catch-up for missed fires, cleared on a fresh session (persisted on resume). Use for periodic checks during a working session; not for guaranteed unattended runs.

### Background monitor / tasks — watch a long-running process
Background Bash processes (a dev server, a watch build) and the Monitor mechanism let Claude start something long-running and be notified on change rather than polling. Use when you're waiting on a process, not a clock.

### Routines (Claude Code on the web) — unattended, durable
Cloud-based scheduled/triggered runs on Anthropic infrastructure — **no local machine or open session required**. Trigger on a schedule (min ~1 hour), on GitHub events (PR opened/labeled/synchronized, release published), or via an API `POST /fire` endpoint. Runs clone the repo fresh and (by default) can only push to `claude/`-prefixed branches. Use for nightly jobs and event-driven automation that must run whether or not you're around.

### Headless `claude -p` — scripted / CI automation
`claude -p "prompt" [flags]` runs non-interactively for scripts and CI/CD. Useful flags: `--allowedTools`, `--permission-mode acceptEdits`, `--output-format json`, `--json-schema`, `--continue`/`--resume`, `--append-system-prompt`. Add `--bare` to skip hooks/skills/plugins/MCP/CLAUDE.md for reproducible CI. Accepts piped stdin. Exit code is non-zero on error — usable as a pipeline gate.

### GitHub Actions — repository-event, team-wide
For repo-native automation (on push/PR/schedule) integrated with CI, drive Claude Code from a GitHub Actions workflow. Persistent and team-wide; independent of any local machine.

## Choosing

| Need | Reach for |
|---|---|
| Orient/setup at session start | SessionStart hook |
| Babysit a PR/deploy/CI right now, interactively | `/loop` (dynamic interval) |
| Periodic check during a working session | Scheduled tasks (Cron) |
| Wait on a long-running process | Background monitor |
| Run nightly / on GitHub events, unattended | Routines (web) |
| A step inside a script or CI pipeline | Headless `claude -p` |
| Repository-event automation for the team | GitHub Actions |

Decision axes: **does it need my machine on? a session open? how durable must it be?** Prefer the lightest mechanism — a SessionStart hook over a loop, a loop over a Routine — and only reach for cloud Routines or Actions when work must run unattended and persist. For PR babysitting specifically, `/loop` with a dynamic interval plus a `.claude/loop.md` default prompt is the sweet spot.
