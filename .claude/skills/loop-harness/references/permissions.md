# Permissions & settings.json

How to configure what Claude Code may do in a project, safety-first. The permission system is the harness's hard floor: get it right and Claude moves fast on safe work and is stopped cold on dangerous work.

## Where settings live (precedence high → low)

| Scope | File | Committed? | Purpose |
|---|---|---|---|
| Managed | system/MDM | n/a | org policy, cannot be overridden |
| Command-line | `claude --flag` | n/a | one-off overrides |
| Project local | `.claude/settings.local.json` | **gitignored** | personal per-project overrides |
| Project | `.claude/settings.json` | **yes** | team-shared, version-controlled |
| User | `~/.claude/settings.json` | n/a | personal defaults across projects |

Commit `.claude/settings.json`; add `.claude/settings.local.json` to `.gitignore`. **A `deny` rule at any scope overrides an `allow` at any other scope** — deny is absolute.

## The `permissions` object

```json
{
  "permissions": {
    "allow": [],
    "deny": [],
    "ask": [],
    "defaultMode": "default",
    "additionalDirectories": []
  }
}
```

- **`allow`** — operations auto-approved (no prompt).
- **`deny`** — operations blocked entirely. Deny wins over allow and ask.
- **`ask`** — operations that always prompt, even if otherwise allowed.
- **`defaultMode`** — behavior for anything not matched: `default` (prompt on first use of a tool), `acceptEdits` (auto-accept file edits and safe commands), `plan` (read-only exploration). Do **not** ship `bypassPermissions`.
- **`additionalDirectories`** — extra paths Claude may work in beyond the project root.

Evaluation order is **deny → ask → allow**, first match wins.

## Rule syntax

A rule is a tool name, optionally with a matcher in parentheses:

- **Bash** — command matching with `*` wildcards. Space is a word boundary:
  - `Bash(npm run test:*)` or `Bash(npm run test *)` — the test script and subcommands
  - `Bash(git status)` — exact; `Bash(git log *)` — any git log
  - `Bash(rm *)` in `deny` — block `rm`; bare `Bash` in `deny` removes the Bash tool entirely
- **Read / Edit / Write** — gitignore-style path globs. `*` stays within a path segment, `**` crosses directories. Anchors: `/x` = project root, `./x` or `x` = cwd, `~/x` = home, `//x` = absolute:
  - `Read(./src/**)`, `Write(/src/**/*.ts)`
  - `Read(.env)` ≡ `Read(**/.env)` — matches `.env` at any depth
- **WebFetch** — `WebFetch(domain:github.com)`, `WebFetch(domain:*.github.com)` (leading `*` only).
- **MCP** — `mcp__<server>__<tool>`; wildcard tool names (`mcp__github__*`, `mcp__*`) are for deny/ask.
- **Parameter matching** (deny/ask only) — `Agent(model:opus)`, `Bash(run_in_background:true)`.

## Safe defaults (adapt per project)

**Allow** the routine, reversible, read-heavy work: build/test/lint scripts, git read commands (`status`, `log`, `diff`, `branch`), reads under source/tests/docs, writes under source/tests, and the MCP servers the project relies on.

**Deny** — the never-allow floor:

- Secrets & credentials: `Read(.env)`, `Read(**/.env*)`, `Read(**/.ssh/**)`, `Read(**/.aws/**)`, `Read(**/secrets/**)`
- Destructive/privileged shell: `Bash(rm -rf *)`, `Bash(sudo *)`
- Protected config: `Edit(.git/**)`

**Ask** — irreversible or outward-facing, so a human confirms: `Bash(git push *)`, `Bash(npm publish)`, deploy commands, and edits to `package.json`/lockfiles if you want them reviewed.

Keep the allow-list tight and specific; a broad `Bash(*)` allow defeats the point. Prefer adding narrow rules as real needs appear over allowing everything up front.
