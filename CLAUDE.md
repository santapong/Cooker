# Cooker — orientation for Claude

Read this once at the start of any new session. It's deliberately short so it doesn't burn context — deeper detail lives in the linked docs.

## What Cooker is

A web-based **CI/CD management tool** with a graph-based UI for visually building pipelines that build OCI-compliant Docker images, push them to registries, and deploy to Kubernetes across Dev / Staging / Production. Single Go binary serves both the API and the React frontend on port 8080.

## Project layout (the parts you'll touch most)

```
backend/
├── cmd/cooker/main.go            entry point — Load + Validate + server.New + Run
└── internal/
    ├── auth/                     OIDC middleware, RBAC group→role mapping
    ├── builder/  pusher/  deployer/  deploytarget/   strategy adapters (Builder, Pusher, Deployer interfaces)
    ├── config/                   env-var loading + production-mode Validate()
    ├── handler/                  thin HTTP layer (one file per domain)
    ├── service/                  business logic (Executor, AppDeployer, Promoter)
    ├── server/                   Gin router, middleware, WebSocket hub, ticket store, rate limiter
    └── store/                    PipelineStore/RunStore/EnvironmentStore interfaces + memory + postgres impls

frontend/src/
├── auth/                         OIDCProvider (oidc-client-ts), Callback, ProtectedRoute
├── api/client.ts                 typed fetch wrapper; injects Bearer token; triggers signinRedirect on 401
├── hooks/useWebSocket.ts         fetches /api/v1/ws-tickets, then opens WS with ?ticket=
├── stores/                       Zustand stores (one per domain)
└── pages/                        route-level components

deploy/
├── docker/Dockerfile             multi-stage; runs as UID 65532
├── helm/cooker/                  chart with cookerEnv, secretKeyRef, securityContext, NetworkPolicy
└── kubernetes/                   raw manifests (parity with chart for non-Helm users)
```

## Authoritative docs (read these before changing code)

| File | Read when |
|---|---|
| [`docs/reference/architecture.md`](docs/reference/architecture.md) | You need the system map (what calls what) |
| [`docs/reference/design.md`](docs/reference/design.md) | You're adding a new feature — patterns, conventions, "Adding a new feature" checklist at §11 |
| [`docs/guides/UAT.md`](docs/guides/UAT.md) | You're touching anything that affects `make uat-up` |
| [`docs/system-design/`](docs/system-design/README.md) | You want the consolidated 17-chapter system design (overview → C4) |
| [`SECURITY.md`](SECURITY.md) | You're touching auth, CORS, secrets, or the Dockerfile |
| [`backlog.md`](backlog.md) | You're picking the next thing to build, or want to know why something isn't done yet |
| [`docs/product-plan.md`](docs/product-plan.md) | You want the adoption roadmap, monetization strategy, or the UAT→production hosting recommendation |

## Current state (as of the `claude/uat-ready-*` series merging)

- **Auth**: OIDC PKCE end-to-end. Bearer-token API. Dev mode (`COOKER_OIDC_ENABLED=false`) injects a dev admin user.
- **`COOKER_ENV`**: `dev` (default), `uat`, `production`. Production gates strict CORS defaults and `Config.Validate()` startup checks.
- **WebSocket auth**: single-use 60s tickets via `POST /api/v1/ws-tickets` then `?ticket=<value>`.
- **Rate limiting**: per-user, in-memory, on `pipelines/:id/run`, `docker/images/build`, `apps/:id/deploy`. Disable for multi-replica.
- **Container**: non-root UID 65532. UAT compose adds host docker GID via `group_add` (Makefile auto-detects).
- **Helm**: `secretKeyRef` for OIDC client secret and `COOKER_SECRET_KEY`. Pod `securityContext` + `NetworkPolicy`, both gated by values.

The honest production-readiness verdict and the open work are in `backlog.md`'s top section — read that before claiming anything is "done."

## Conventions

### Backend (Go)
- **Layering**: handler → service → store/strategy. Handlers do HTTP parsing only. Services hold business logic. Adapters implement narrow interfaces.
- **Errors**: wrap with package prefix — `fmt.Errorf("oidc: discover: %w", err)`. The store package exposes typed `ErrNotFound`; check via `errors.Is`.
- **Tests**: every `internal/<pkg>/*.go` ships with a `*_test.go` for non-trivial logic. Race detector is on in CI: `go test ./... -race`. `go vet ./...` runs before tests.
- **Adding a new pluggable backend** (e.g. a new builder): implement the interface, add a constructor case to `selectXxx` in `server.go`, document the env-var value in `.env.uat.example` and `docs/guides/UAT.md`.
- **No business logic in handlers. No HTTP types in services. No `panic` outside startup.**

### Frontend (TypeScript)
- `strict: true`, `noUnusedLocals: true`. `tsc --noEmit` runs in CI.
- `OIDCProvider` exports both the React provider AND module-level helpers (`getAccessToken`, `triggerSignIn`) — the API client consumes the helpers without going through React context. Keep that pattern.
- **No `localStorage` outside `auth/`** — token storage is owned by `oidc-client-ts`; everything else uses Zustand.
- **No backend URLs in components** — all HTTP goes through `api/`.

### Branching and PRs
- Branch from `main`. Names: `claude/<topic>` or `<area>/<topic>`.
- One PR per logical change; squash-merge.
- Never push to `main` directly.
- For stacked work: parent PR's branch is fine as the base, but rebase forward as parents merge (use `git rebase --onto main <parent>` or cherry-pick).
- All PRs draft until ready for review.

### CI
- `.github/workflows/ci.yml` runs on PRs to `main` and `claude/**`.
- Backend job: `go build` → `go vet` → `go test -race` (against a Postgres service).
- Frontend job: `npm ci` → `npm run lint` → `npm run build` → `npm test`.
- Docker job: `docker build` against `deploy/docker/Dockerfile`.

## Workflow expectations

1. **Read the relevant doc(s) above before writing code** — `docs/reference/design.md` §11 has the "adding a feature" checklist.
2. **Use `TodoWrite`** for any task with 3+ steps. Mark in-progress before starting, completed immediately after.
3. **Run `make test` (or the targeted test command) before pushing**. Don't wait for CI to catch test failures.
4. **For changes to auth, secrets, or the Dockerfile**, also update `SECURITY.md` so the threat model stays accurate.
5. **For changes to UAT behaviour**, update `docs/guides/UAT.md` in the same PR.
6. **When a `backlog.md` item lands on `main`**: in the same PR, move the item from its priority section into the "Closed" log at the bottom and reference the merged PR number.

## What NOT to do without asking

- Don't reintroduce `Allow-Credentials: true` (PR A removed it deliberately).
- Don't bind-mount `/var/run/docker.sock` in any new context — it's the open issue Kaniko (P1.1) closes.
- Don't put `COOKER_OIDC_ENABLED=true` in UAT compose — UAT is auth-off by design; toggling it requires `.env.uat` config (Google or KeepSave preset).
- Don't change `COOKER_ENV` defaults globally; production-mode strictness is gated by it on purpose.
- Don't add new fields to `internal/handler/*.go` requests without a corresponding store migration in `internal/store/postgres/migrations/`.
- Don't bump Go past 1.22 without bumping `golang.org/x/time` in lockstep — currently pinned at v0.5.0 because v0.15+ requires Go 1.25.

## Open backlog highlights

The full list is in `backlog.md`. The three highest-impact items I scoped in detail are:

1. **P1.1 — Kaniko builder adapter.** Closes the docker.sock RCE-to-host gap. ~half a day.
2. **P1.2 — Audit logging middleware.** Per-route opt-in slog audit trail. ~2 hours.
3. **P6.1 — `helm lint` + `helm template` + `kubeconform` in CI.** ~10 minutes; YAML is in the backlog ready to drop in.

**KeepSave secrets manager (P2)** ships at HEAD — adapter at `backend/internal/secrets/keepsave/` (~457 LOC), Helm wiring renders `COOKER_SECRETS_KEEPSAVE_{URL,PROJECT_ID,API_KEY}` (API key via `secretKeyRef`), and `Config.Validate()` (`backend/internal/config/config.go:413-423`) enforces the required env vars before boot. Select it with `COOKER_SECRETS_BACKEND=keepsave` (chart: `secrets.backend=keepsave`).
