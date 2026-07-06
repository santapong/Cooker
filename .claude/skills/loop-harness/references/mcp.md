# MCP Servers (.mcp.json)

MCP (Model Context Protocol) servers give Claude Code external tools and data — GitHub, a database, an internal API, a search index. Project servers are declared in `.mcp.json` at the repo root and committed so the whole team gets them.

## Schema

```json
{
  "mcpServers": {
    "<server-name>": { "…transport config…" }
  }
}
```

Each server uses one transport:

- **stdio** (local process) — `command` (required), `args`, `env`:
  ```json
  {
    "local-tools": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "${CLAUDE_PROJECT_DIR}"],
      "env": { "LOG_LEVEL": "info" }
    }
  }
  ```
- **http** (remote, recommended for hosted) — `type: "http"`, `url`, optional `headers`:
  ```json
  {
    "remote-api": {
      "type": "http",
      "url": "${API_BASE_URL:-https://api.example.com}/mcp",
      "headers": { "Authorization": "Bearer ${API_TOKEN}" }
    }
  }
  ```
- **sse** / **ws** also exist (`type: "sse"` / `"ws"` with `url`), but prefer `http` for new remote servers.

## Environment variable expansion

`${VAR}` and `${VAR:-default}` expand in `command`, `args`, `env`, `url`, and `headers`, at session start. **Never hardcode tokens** — reference them: `"Authorization": "Bearer ${API_TOKEN}"`. If a required var is unset and has no default, config parsing fails (a useful fail-fast).

## Scopes

| Scope | Stored in | Shared? | Add with |
|---|---|---|---|
| local | `~/.claude.json` (per project) | no (private) | `claude mcp add --scope local …` |
| **project** | **`.mcp.json`** | **yes (committed)** | `claude mcp add --scope project …` |
| user | `~/.claude.json` (user-wide) | no | `claude mcp add --scope user …` |

For a team harness, prefer **project** scope (`.mcp.json`). Precedence when names collide: local → project → user → plugin-provided.

## Trust & enabling

Project `.mcp.json` servers require a workspace-trust approval the first time a repo is opened (a security default, since a server can run code). Options:

- Approve interactively when prompted.
- Set `"enableAllProjectMcpServers": true` in `settings.json` to auto-approve this project's `.mcp.json` servers (only do this for a repo you trust).
- Reset choices with `claude mcp reset-project-choices`.

## CLI quick reference

```bash
claude mcp add --transport stdio <name> -- <command> [args…]
claude mcp add --transport http  <name> <url>
claude mcp list
claude mcp get <name>
claude mcp remove <name>
```

Keep secrets out of the committed `.mcp.json` by using `${VAR}` expansion and providing the actual values via the environment (or `.claude/settings.local.json` `env`, which is gitignored).
