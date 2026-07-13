# Acceptance criteria — restructure & deploy-simplification

Binary, checkable conditions for the five-part effort (backend review, easy deploy, frontend reset, backend restructure, tech-debt hunt). A phase's PR is mergeable only when all of its criteria hold. Boxes are checked as each phase lands.

| Phase | PR | Base |
|---|---|---|
| 0+1 — review + easy deploy | #150 | `main` |
| 2 — frontend reset | (in #150) | — |
| 3a — safe splits | #151 | #150 |
| 3b — handlers → methods | #152 | #151 |
| 3c — domain regroup | (this) | #152 |

## AC-0 — Backend review & tech-debt report (asks #1, #5)
- [x] `docs/reference/backend-review.md` documents (a) current best-practice patterns and (b) a ranked tech-debt list.
- [x] Every tech-debt item names a concrete file (and line where relevant) a reader can open.
- [x] Each ranked item maps to a phase that addresses it (or is explicitly deferred).

## AC-1 — Easy deploy (ask #2)
- [ ] `make deploy-docker` on a clean host brings up **three separate services** (app + postgres + redis); `curl localhost:8080/health` returns 200.
- [ ] First `make deploy-docker` generates `.env.prod` with fresh random secrets; `.env.prod` is git-ignored and never committed.
- [ ] The stack boots under `COOKER_ENV=production` (passes `Config.Validate()`): TLS Postgres over `sslmode=require`, local auth enabled, no dev-admin injection.
- [ ] A pipeline run created via the API survives `docker compose -f docker-compose.prod.yml restart` (proves Postgres persistence, not the memory store).
- [ ] `make deploy-k8s` installs the chart with `values-quickstart.yaml`; the pod reaches Ready with **no externally-provisioned database**; `helm template | kubeconform` (via `make helm-validate`) passes.
- [ ] `cooker migrate up` (and `make migrate-up`) applies migrations without starting the server; `make migrate-down` prints manual-rollback guidance rather than silently booting.
- [ ] No doc still claims the SPA is `//go:embed`-ed; `COOKER_STATIC_DIR` overrides the static root (default `/usr/share/cooker/static`).
- [x] `go build ./... && go vet ./...` and the changed-package tests pass.

## AC-2 — Frontend reset (ask #3)
- [x] `design_handoff_cosmic_theme/` and the cosmic design surface (`theme/tokens.ts`, `components/ui/`, `components/layout/`, `components/pipeline|compose/` visuals, `Skeleton`, `FeedbackButton`) are deleted.
- [x] All plumbing is intact and untouched in behavior: `api/`, `auth/` (OIDCProvider, Callback, ProtectedRoute logic), `hooks/` (esp. `useWebSocket`), `stores/`, `types/`, `utils/`.
- [x] Every route in `App.tsx` still resolves to a component (placeholder stubs); the route table and provider nesting are preserved.
- [x] `cd frontend && npx tsc --noEmit && npm run build && npm test && npm run lint` all pass (lint: 0 errors).
- [ ] `npm run dev` serves the app; the auth redirect and a WebSocket connection still function.

## AC-3a — Safe splits (ask #4, part 1)
- [x] The four biggest God-files are decomposed within their packages (`executor.go`, `config.go`, `memory.go`, `server.go`).
- [x] `store.New` takes a single `store.Components` struct; no caller passes positional store args.
- [x] Pure refactor: `go build ./... && go vet ./...` and `config`/`store`/`service`/`server`/`handler` tests pass; `gofmt`/`goimports` clean; no behavior/API change.

## AC-3b — Handlers → methods (ask #4, part 2)
- [x] No route handler in `docker.go`/`registry.go`/`kubernetes.go` is a bare package function; all are `*Handler` methods (helpers may stay package funcs).
- [x] No package-level mutable global remains in the handler layer (`composeBaseDir` is a `Handler` field; `SetComposeBaseDir` is gone).
- [x] `router.go` references no `handler.<Func>` package function for these routes; the unused `handler` import is removed.
- [x] `go build ./... && go vet ./...` and `handler` + `server` tests pass; same routes/responses as before.

## AC-3c — Domain regroup (ask #4, part 3)
- [x] `builder`, `pusher`, `stagerunner`, `oci`, `buildplan` live under `internal/build/`; `deployer`, `deploytarget` under `internal/deploy/`; `cloudinventory` under `internal/cloud/`; `notifier` under `internal/notify/`.
- [x] `handler/`, `service/`, `store/`, `model/` layer packages are unchanged (layering preserved).
- [x] Every moved package's import path is updated repo-wide; `go build ./...` reports **no** unresolved import and **no** import cycle.
- [x] `go vet ./... && go test ./...` pass after each individual move (each move is its own green commit).
- [x] `CLAUDE.md` Project-layout, `docs/reference/*`, and the `.claude/` agent + skill routing paths reflect the new paths.
- [ ] No behavior change: full UAT smoke (`make uat-up` → `make test-e2e`) passes unchanged (reviewer/CI-verified — not runnable in this sandbox).

## AC — Cross-cutting (all phases)
- [x] Each phase lands as a **draft PR** with CI green before merge (round 1: #150 → #151 → #152 → #153, all merged).
- [x] No secret, token, or model identifier is committed to any repo artifact.
- [ ] `make test` and `make lint` run clean locally before each push.

---

# Round 2 — Deploy editions, ENV/proxy feature, full-scan report

## AC-4 — Deploy editions LIGHT / FULL
- [ ] `make deploy-docker-light` boots with **no** Redis container, memory ws/ticket/rate backends, and jobqueue/scheduler/metrics/tracing off; passes production `Validate()`.
- [ ] `make deploy-docker-full` (base + `docker-compose.full.yml` overlay) boots Redis with redis backends, jobqueue + scheduler + metrics + tracing on, audit `stdout,db` + retention.
- [ ] Switching editions in place replaces the marker-delimited preset block in `.env.prod` (no duplicate keys) and preserves secrets/volumes.
- [ ] `values-light.yaml` / `values-full.yaml` render via the CI helm job; `make deploy-k8s-light|full` targets exist.
- [ ] INSTALL.md documents both editions, the in-place upgrade path, and that license tiers are orthogonal.

## AC-5 — ENV injection + DeployedURL + reverse proxy
- [ ] An app linked to an Environment receives its `PlainVars` + decrypted `Secrets` as container env on the docker, ssh, and k8s deploy paths; stage-explicit `Config.Env` keys win on conflict; no environment linked → unchanged behavior.
- [ ] With `COOKER_PROXY_DOMAIN` set, a successful deploy stores `App.DeployedURL`, returns `url` in the deploy response, and the frontend renders an "Open app" link.
- [ ] Without a proxy domain, docker/ssh deploys with published ports still derive `http://<host>:<port>`.
- [ ] Traefik overlay (compose `proxy` profile) routes `<slug>.<domain>` to the deployed container; dockerrun/compose deployers attach the labels + proxy network only when the domain is configured.
- [ ] K8s manifest synthesis emits an `Ingress` (host `<slug>.<domain>`, configurable `ingressClassName`) only when the domain is set.
- [ ] New unit tests green (env merge precedence, deployer label/env assertions, Ingress + URL table tests); UAT e2e unchanged.

## AC-6 — Full-scan technical report (report only)
- [ ] `docs/audits/2026-07-full-scan-report.md` exists with one section per file type (Go, TS/TSX, SQL, YAML, Dockerfile, Shell, Terraform, JS/other), each containing performance findings, vulnerability findings, a severity table, and file:line evidence.
- [ ] Tool outputs captured: `golangci-lint`, `govulncheck`, `gosec`, `npm audit`, plus grep-based secret scan; each pass's status embedded.
- [ ] Zero source-code changes in the scan PR (`git diff` = report + backlog/AC checkboxes only); doc-links check green.
- [ ] Findings cross-checked against `docs/audits/*` so closed items aren't re-flagged; report ends with a ranked remediation order.
