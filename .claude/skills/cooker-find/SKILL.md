---
name: cooker-find
description: Fast navigation in the Cooker codebase — given a feature, intent, or symptom, route to the right file in seconds. Trigger on "where is X", "how does Y work", "which file owns Z", "find the code that does …", or any locate-style question about Cooker's layout. Use this BEFORE running blind grep sweeps.
---

# Cooker — fast finder

Cooker is a Go-backend + React-frontend CI/CD platform. Single binary on port 8080 serves both.

## Who-owns-what

| Asking about | Look in |
|---|---|
| HTTP endpoint for `/api/v1/<domain>/…` | `backend/internal/handler/<domain>.go` |
| Business logic for pipeline runs / app deploys | `backend/internal/service/` (`executor.go`, `app_deployer.go`, `promoter.go`) |
| Database access (CRUD) | `backend/internal/store/postgres/<domain>.go` (memory mirror in `store/memory/memory.go`) |
| Image-build adapters (Kaniko / Buildah / BuildKit / docker.sock) | `backend/internal/build/builder/` |
| Push-to-registry adapters | `backend/internal/build/pusher/` |
| Deploy adapters (clientgo / Helm / cloud targets) | `backend/internal/deploy/deployer/` |
| Auth (OIDC + local) + RBAC | `backend/internal/auth/` |
| WebSocket hub, ticket store, rate limiter | `backend/internal/server/{websocket,wshub_backend,wsticket,ratelimit}*.go` |
| Run lifecycle, heartbeat, orphan sweep | `backend/internal/server/runs.go` + `store/postgres/run.go` |
| Schema migrations | `backend/internal/store/postgres/migrations/*.sql` (both `.up` and `.down`) |
| Config loader + validation | `backend/internal/config/config.go` |
| Cross-cutting helpers | `backend/internal/{retry,validate,idempotency,crypto,observability,audit}/` |
| Frontend pages | `frontend/src/pages/` |
| Frontend API client | `frontend/src/api/client.ts` |
| Helm chart | `deploy/helm/cooker/` |
| Raw K8s manifests (non-Helm operators) | `deploy/kubernetes/` |
| Multi-stage container build | `deploy/docker/Dockerfile` |
| CI workflows | `.github/workflows/` |

## Read these before changing code

| File | When |
|---|---|
| `CLAUDE.md` | Always — orientation + conventions |
| `docs/architecture.md` | Need the system map (what calls what) |
| `docs/design.md` § 11 | Adding a new feature; checklist |
| `docs/audits/remediation-plan.md` | Picking a known fix (themes T1–T24) |
| `docs/audits/chain-recheck.md` | Current state of every chain failure (Open / Closed / Mitigated) |
| `docs/audits/{dag-performance,spof-and-database,crash-and-service-quality,vulnerabilities-and-chains}.md` | Detailed findings for an area |
| `docs/UAT.md` | Touching `make uat-up` |
| `docs/RUNBOOK.md` | Runbook + alert reference |
| `SECURITY.md` | Auth, CORS, secrets, Dockerfile changes |
| `backlog.md` | Why something isn't done yet |

## Workflow

1. Identify the area (handler / service / store / adapter / config / ops).
2. **Run `.claude/skills/cooker-find/where-is.sh <noun>`** — resolves a domain word into the canonical file paths from the table below. Falls back to ripgrep when the noun isn't curated.
3. Check the right audit doc for known issues and citations.
4. Use **Read**, never `cat`, to open the file.
5. If the question is broad ("how does X work end-to-end"), spawn an Explore subagent rather than reading 10 files in the main context.

### Bundled scripts

| Script | What it does |
|---|---|
| `where-is.sh <noun>` | Print the canonical file paths for a domain word (`pipeline`, `kaniko`, `oidc`, `migration`, `audits`, …). Use this **before** composing a `rg` invocation; the curated map is cheaper than searching. |

## Common ripgrep recipes

```bash
# All HTTP handlers for a noun
rg -n 'func \(h \*Handler\) [A-Z][A-Za-z]*\(c \*gin\.Context' backend/internal/handler

# Where a model field is read or written
rg -n '\.Stages\b' backend

# Every place a typed error is raised
rg -n 'store\.(ErrNotFound|ErrConflict)' backend

# Every place that wraps the same package's errors
rg -n 'fmt\.Errorf\("[a-z_]+:' backend

# All env-var consumers (used during config audits)
rg -n 'getEnv\("|os\.Getenv\(' backend/internal/config

# Audit theme references
rg -n 'T(1[0-9]|2[0-4]|deadline)\b' docs/audits backend

# Every Update path in the postgres store (for optimistic-concurrency pattern)
rg -n 'WHERE id=\$1 AND version=' backend/internal/store/postgres
```

## Anti-patterns

- Don't `cat` files. Use Read.
- Don't grep for filenames; find them via the table above or via a symbol.
- Don't tail-search files — `rg` for the call expression instead.
- Don't read the whole audit-doc set "for context"; pick the one section that matches the area.

## When the question is "find a bug" not "where is X"

Use **`cooker-audit`** instead. That skill bundles the anti-patterns from the audit series + a `audit-greps.sh` that runs the canonical investigative greps. `cooker-find` answers *known-target* questions; `cooker-audit` answers *find-the-target* questions.

## Escape hatch

If the question is *"this used to work last week, where did it break?"*:

```bash
git log -p -S '<symbol>' --since='14 days ago' -- backend
git bisect start; git bisect bad; git bisect good <last-known-good-tag>
```
