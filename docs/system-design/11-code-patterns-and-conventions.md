# 11 · Code Patterns & Conventions

> **Purpose:** the patterns and rules a contributor needs in one place — what the code looks like and
> what's enforced. **See also:** [`../design.md`](../reference/design.md) (don't duplicate it; this links to it),
> esp. §11 "adding a feature".

## Design patterns

| Pattern | Why | Where (real code) |
|---|---|---|
| **Strategy** | Swap build/push/deploy backends at startup | `internal/{builder,pusher,deployer,deploytarget}` + `selectXxx` in `internal/server/server.go` |
| **Repository** | Same store API over Postgres or in-memory | `internal/store/store.go` interfaces; `memory/` + `postgres/` |
| **Functional options** | Extensible constructors without breaking callers | `service.NewExecutor(WithBuilder(...), WithPusher(...), WithDeployer(...))` |
| **Middleware** | Compose cross-cutting HTTP concerns | CORS / OIDC / RBAC wiring in `server.go` + `internal/auth` |
| **Observer / pub-sub** | Fan out live updates to N clients | WebSocket hub (+ Redis) — see [07](07-realtime-and-concurrency.md) |
| **Fail-fast init** | Surface config/connection errors at boot, not mid-request | `main.go` Load→Validate→New; OIDC discovery; Postgres ping |
| **Embedded resources** | Single self-contained binary | `//go:embed migrations/*.sql` + embedded SPA |
| **Provider + module helpers** | Read auth token outside React context | `OIDCProvider` exporting `getAccessToken`/`triggerSignIn` |
| **Route guard** | Gate routes on auth + role | `ProtectedRoute.tsx` |
| **Domain stores** | One Zustand store per domain, no cross-imports | `frontend/src/stores/*` |
| **Typed API client** | Central transport, typed per domain | `frontend/src/api/client.ts` + domain modules |

## Go style rules

- **Strict layering:** handler → service → store. No HTTP types in services. No business logic in
  handlers. **No `panic` outside startup.**
- **Error wrapping:** `fmt.Errorf("pkg: action: %w", err)` — package-prefixed, wrapping the cause.
- **Typed sentinels:** `store.ErrNotFound` checked via `errors.Is` (maps to 404 at the edge).
- **Formatting:** `gofmt` — zero drift.
- **Tests:** every non-trivial package ships `*_test.go`; the race detector is on (`go test -race`).

## Frontend style rules

- `tsconfig`: `strict: true`, `noUnusedLocals`, `noUnusedParameters`.
- **No `localStorage` outside `auth/`** — token storage is owned by `oidc-client-ts`; everything else
  is Zustand.
- **No backend URLs in components** — all HTTP goes through `api/`.
- The **OIDCProvider module-helper** pattern (`getAccessToken`/`triggerSignIn`) is intentional; the
  ESLint config keeps `@typescript-eslint/no-explicit-any` **off** to accommodate it.

## Linters & configs

- **Go:** `backend/.golangci.yml`, golangci-lint **v2.5.0**. Enabled linters: `errcheck`, `govet`,
  `ineffassign`, `staticcheck`, `unused`, `bodyclose`, `misspell`, `unconvert` (staticcheck runs
  `all` minus a few pre-existing-churn checks; `errcheck` excludes `fmt.Fprintf`/`Fprintln`).
- **Frontend:** `frontend/eslint.config.js` — `typescript-eslint` + `react-hooks` +
  `react-refresh`.

## Enforced vs convention vs aspirational

Be honest about what actually blocks a merge:

| Rule | How it's enforced |
|---|---|
| `gofmt -l .` clean | **CI-gated** (fails the build) |
| `go vet ./...` | **CI-gated** |
| `go test -race` (against Postgres) | **CI-gated** |
| Frontend `tsc` / `npm run build` | **CI-gated** |
| `npm run lint` (ESLint) | **CI-gated** |
| golangci-lint v2.5.0 | **Aspirational** — step runs with `continue-on-error: true`, so a lint failure does **not** block the job today |
| Strict layering, error-wrap style, no-`panic` | Convention / code review |
| No `localStorage` outside `auth/`, no backend URLs in components | Convention / code review |
| One-file-per-domain handlers | Convention / code review |
| PR/branch rules below | Convention (+ branch protection on `main`) |

## Naming & organization

- **Handlers:** one file per domain in `internal/handler/` (`pipeline.go`, `app.go`, …).
- **Pluggable backends:** the `selectXxx` recipe — implement the interface → add a `case` → document
  the env var in `.env.uat.example` + [`../UAT.md`](../guides/UAT.md) → add a contract test (see
  [05-extension-points.md](05-extension-points.md)).
- **Branches:** `claude/<topic>` or `<area>/<topic>`, branched from `main`.
- **Checklists:** "adding a feature" and "adding a pluggable backend" live in
  [`../design.md`](../reference/design.md) §11.

## PR conventions

- Branch from `main`; **one PR per logical change**; **squash-merge**.
- **All PRs draft until ready** for review.
- **Never push to `main` directly.**
- For stacked work, a parent PR's branch is a fine base — but rebase forward as parents merge
  (`git rebase --onto main <parent>` or cherry-pick).

---

> _Verified against `main` @ `dd93402` on 2026-05-30. If you change the described behaviour, update this chapter in the same PR._
