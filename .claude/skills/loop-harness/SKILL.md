---
name: loop-harness
description: Engineer a project's Claude Code harness — permissions, hooks, MCP servers, and automation loops — from reusable scaffolds. Use when the user wants to set up or harden a project's .claude/settings.json, add or design hooks, configure permissions (allow/deny/ask), wire up MCP servers via .mcp.json, or automate recurring work (SessionStart setup, /loop, scheduled tasks, PR-watch, headless runs). Ships copy-paste settings.json, .mcp.json, hook scripts, and a loop scaffold.
---

# Engineering Harnesses

You are about to shape the *harness* a Claude Code project runs inside — the configuration that decides what Claude may do (permissions), what happens automatically around its actions (hooks), what external capabilities it has (MCP), and how work repeats without a human re-typing it (automation loops). A good harness makes the right thing automatic and the dangerous thing impossible; a bad one either nags on every action or quietly permits a `rm -rf`.

**Design the harness safety-first and project-shared: deny is stronger than allow, secrets are never readable, and everything a teammate needs is committed.** This skill ships copy-paste scaffolds you adapt, not theory to reimplement.

## 1. The four pillars, and where they live

| Pillar | File | Committed? |
|---|---|---|
| **Permissions** — what Claude may do without asking | `.claude/settings.json` | yes (team) |
| **Hooks** — scripts that fire around events | `.claude/settings.json` + `.claude/hooks/*.sh` | yes (team) |
| **MCP servers** — external tools/data | `.mcp.json` | yes (team) |
| **Automation loops** — recurring / unattended work | `/loop`, scheduled tasks, hooks, Routines | varies |

Personal, machine-specific overrides go in `.claude/settings.local.json` (gitignored). Start from **`templates/settings.json`** and pare it to the project.

## 2. Permissions — safety-first

Full rules and rule syntax in **`references/permissions.md`**; scaffold in **`templates/settings.json`**.

- The `permissions` object has `allow`, `deny`, `ask` arrays plus `defaultMode`. **`deny` beats `allow` from any scope** — deny is your hard floor.
- Rules are tool-scoped with argument matching: `Bash(npm run test:*)`, `Read(./src/**)`, `Write(/src/**)`, `WebFetch(domain:github.com)`, `mcp__github__*`.
- **Never-allow list** (put in `deny`): reading secrets (`Read(.env)`, `Read(**/.env*)`, `Read(**/.ssh/**)`, `Read(**/.aws/**)`), and destructive shell (`Bash(rm -rf *)`, `Bash(sudo *)`). Route irreversible/outward actions (`git push`, publish, deploy) through `ask`.
- Pick `defaultMode` deliberately: `default` (prompt on first use), `acceptEdits` (auto-accept edits), `plan` (read-only). Do not ship `bypassPermissions`.

## 3. Hooks — automate around events

Full event list, input JSON, and exit-code control in **`references/hooks.md`**; example scripts in **`templates/hooks/`**.

- Configured under `hooks` in `settings.json`, keyed by event → array of `{ matcher, hooks: [{ type: "command", command }] }`.
- A hook reads a JSON event on **stdin** and controls flow by **exit code**: `0` = proceed (stdout JSON may add `additionalContext` or a `permissionDecision`); **`2` = block**, with stderr fed back to Claude; other = non-blocking warning.
- Reference project files with `${CLAUDE_PROJECT_DIR}` and keep scripts in `.claude/hooks/` (make them executable).
- Highest-value recipes (shipped as templates): **SessionStart** to inject project context/setup, **PreToolUse** to guard secrets and dangerous commands, **PostToolUse** to auto-format edited files.

## 4. MCP servers

Schema, transports, scopes, and trust in **`references/mcp.md`**; scaffold in **`templates/mcp.json`** (copy to `.mcp.json` at the project root).

- `.mcp.json` holds an `mcpServers` map. Each server is **stdio** (`command`, `args`, `env`), **http** (`type: "http"`, `url`, `headers`), or sse/ws.
- Use `${VAR}` / `${VAR:-default}` expansion for secrets in `env`/`headers` — never hardcode tokens.
- Project `.mcp.json` servers require workspace trust on first use; `enableAllProjectMcpServers` in settings controls auto-approval.

## 5. Automation loops — pick the right mechanism

Decision guide in **`references/automation-loops.md`**; default `/loop` prompt scaffold in **`templates/loop.md`**.

Choose by *does it need the machine on / a session open, and how durable must it be?*

| Mechanism | Needs session open | Best for |
|---|---|---|
| **SessionStart hook** | n/a (runs at start) | setup/context on every session |
| **`/loop [interval] [prompt]`** | yes | babysitting a PR/deploy/CI in-session |
| **Scheduled tasks** (Cron tools) | yes (fires when idle) | periodic checks during a working session |
| **Routines** (Claude Code on the web) | no (cloud) | unattended nightly jobs, GitHub-event automation |
| **Headless `claude -p`** | no (scripted) | CI/CD steps, batch automation |
| **GitHub Actions** | no | repository-event and team-wide automation |

Prefer the *lightest* mechanism that meets the durability need: a SessionStart hook over a loop, a loop over a cloud Routine, unless persistence or unattended execution is actually required.

## 6. Assemble and install the scaffold

Copy the scaffolds, then adapt:

```
.claude/
├── settings.json        # from templates/settings.json — pare permissions to the project
├── settings.local.json  # personal overrides — add to .gitignore
├── hooks/
│   ├── session-start.sh # from templates/hooks/
│   └── guard-secrets.sh
└── loop.md              # from templates/loop.md — default /loop prompt
.mcp.json                # from templates/mcp.json — only if the project uses MCP
```

Commit `settings.json`, `hooks/`, `loop.md`, and `.mcp.json`; gitignore `settings.local.json`. Validate the result with `claude` (it will report invalid settings) and test each hook script by piping it a sample event.

## Reference files

- `references/permissions.md` — settings.json locations/precedence, allow/deny/ask, rule syntax, safe defaults
- `references/hooks.md` — hook events, settings structure, stdin input, exit-code control, common recipes
- `references/mcp.md` — .mcp.json schema, transports, scopes, env expansion, trust
- `references/automation-loops.md` — every recurring/automation mechanism and how to choose
- `references/standards.md` — the authoritative standards this skill applies — named, version-pinned, and mapped to its workflow
- `templates/settings.json` — copy-paste project harness (permissions + hooks + env)
- `templates/mcp.json` — copy-paste `.mcp.json` (stdio + http servers with env expansion)
- `templates/hooks/session-start.sh`, `templates/hooks/guard-secrets.sh` — example hook scripts
- `templates/loop.md` — default `/loop` prompt scaffold
