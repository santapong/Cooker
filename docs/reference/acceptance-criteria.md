# Acceptance criteria — restructure & deploy-simplification

> **Validation:** every item was independently re-verified with fresh evidence on 2026-07-13 — see [`ac-validation-report.md`](ac-validation-report.md) (37 PASS / 9 DEFERRED-live / 1 BLOCKED-CI / 1 FAIL, with per-item commands and the quantitative change report).

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
- [x] First `make deploy-docker` generates `.env.prod` with fresh random secrets; `.env.prod` is git-ignored and never committed. *(validated 2026-07-13)*
- [ ] The stack boots under `COOKER_ENV=production` (passes `Config.Validate()`): TLS Postgres over `sslmode=require`, local auth enabled, no dev-admin injection.
- [ ] A pipeline run created via the API survives `docker compose -f docker-compose.prod.yml restart` (proves Postgres persistence, not the memory store).
- [ ] `make deploy-k8s` installs the chart with `values-quickstart.yaml`; the pod reaches Ready with **no externally-provisioned database**; `helm template | kubeconform` (via `make helm-validate`) passes.
- [x] `cooker migrate up` (and `make migrate-up`) applies migrations without starting the server; `make migrate-down` prints manual-rollback guidance rather than silently booting. *(validated 2026-07-13: binary exits 2 on down, applies on up)*
- [x] No doc still claims the SPA is `//go:embed`-ed; `COOKER_STATIC_DIR` overrides the static root (default `/usr/share/cooker/static`). *(validated 2026-07-13)*
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
- [x] Switching editions in place replaces the marker-delimited preset block in `.env.prod` (no duplicate keys) and preserves secrets/volumes. *(validated 2026-07-13: light→full→light = 1 block, secrets unchanged)*
- [ ] `values-light.yaml` / `values-full.yaml` render via the CI helm job; `make deploy-k8s-light|full` targets exist.
- [x] INSTALL.md documents both editions, the in-place upgrade path, and that license tiers are orthogonal. *(validated 2026-07-13)*

## AC-5 — ENV injection + DeployedURL + reverse proxy
- [x] An app linked to an Environment receives its `PlainVars` + decrypted `Secrets` as container env on the docker, ssh, and k8s deploy paths; stage-explicit `Config.Env` keys win on conflict; no environment linked → unchanged behavior.
- [x] With `COOKER_PROXY_DOMAIN` set, a successful deploy stores `App.DeployedURL` and the frontend renders an "Open app" link. (Deviation from the original wording: the async deploy 202 does NOT carry a `url` — for compose apps the per-service host isn't knowable pre-clone, so the authoritative URL is surfaced via `GET /apps/:id`.deployedURL instead of a possibly-wrong prediction.)
- [x] Without a proxy domain, docker deploys with published ports still derive `http://localhost:<host-port>` (single-host fallback; no host field exists on DeployTarget).
- [ ] Traefik overlay (`docker-compose.proxy.yml`) routes `<slug>.<domain>` to the deployed container — label + network attachment is unit-verified; live routing needs a docker host (reviewer/UAT-verified).
- [x] dockerrun deploys attach Traefik labels + the proxy network only when the domain is configured (`TestDockerRunArgs_LabelsAndNetwork`, ProxyHost stamping tests).
- [x] K8s manifest synthesis emits an `Ingress` (host `<slug>.<domain>`, configurable `ingressClassName`) only when the domain is set.
- [x] New unit tests green (env merge precedence, resolver nil-safety, deployer label/env assertions, Ingress + URL table tests); full service/store/server/handler suites pass.

## AC-6 — Full-scan technical report (report only)
- [x] `docs/audits/2026-07-full-scan-report.md` exists with one section per file type (Go, SQL, YAML, Dockerfile, Shell, Terraform, JS/TS), each with performance + vulnerability findings, severity tables, and file:line evidence.
- [x] Tool outputs captured: `golangci-lint` (32), `gosec` (57, triaged), `npm audit` (5), secret-grep (clean); `govulncheck` status embedded as **blocked** (proxy 403) with the CI recommendation.
- [x] Zero source-code changes in the scan PR (report + AC checkbox edits only); doc-links check green.
- [x] Findings cross-checked against `docs/audits/*` (closed items excluded, tracked-open labeled); report ends with a ranked remediation order.

---

# Round 3 — Optimization round (O0–O4)

Scoped from the full-scan report's remediation order (items 1–4) plus the resource-recommendation
deliverable; approaches search-validated (semaphore-vs-errgroup for continuous arrivals, Postgres
TOAST write amplification, kaniko `--cache-repo`). One branch, one commit per item.

## AC-7.0 — Resource requirements documented (O0)
- [x] INSTALL.md carries a "Resource requirements" section: per-mode sizing table (LIGHT/FULL docker, proxy overlay, k8s presets, UAT), per-container CPU/RAM breakdown, and sizing rules of thumb. *(validated 2026-07-13)*

## AC-7.1 — Run-concurrency cap (O1, scan item 1 — the only High)
- [x] `COOKER_MAX_CONCURRENT_RUNS` (default 8, `0` = unlimited) bounds in-flight `RunCoordinator` spawns via `semaphore.Weighted.TryAcquire` — reject-when-full, never queue-and-block the handler.
- [x] Saturated spawn returns HTTP **429** with `Retry-After: 30` on pipeline-run, app deploy/redeploy/rollback paths (`handler.ErrRunCapacity` mapping).
- [x] The slot is released when the run goroutine finishes (defer), and rejections increment `cooker_run_capacity_rejected_total`.
- [x] Tests: cap=1 saturation → `ErrRunCapacity`; release frees the slot; nil-semaphore = unlimited back-compat. Race-detector green.

## AC-7.2 — Run-JSONB read/write bloat (O2, scan item 3)
- [x] `RunStore.UpdateProgress(id, stageRuns)` writes ONLY the `stage_runs` column with per-stage logs stripped; wired as `service.WithRunUpdater` in `server.New` (activates the previously-dormant mid-run progress persistence). Logs land exactly once, in the terminal full `Update`.
- [x] `RunStore.GetSummary` strips logs in SQL (`jsonb_agg(elem - 'logs')`, same projection as `List`); the polled `GET /pipelines/:id/runs/:runId` serves it.
- [x] Log consumers unaffected: `GetStageLogs`, run diff, triage, and approval paths stay on full `Get` — pinned by the existing stage-logs test plus a new summary-has-no-logs handler test.
- [x] Memory impls mirror the Postgres semantics (strip-on-read copy; strip-on-write replace); parity tests green.

## AC-7.3 — List-endpoint pagination (O3, scan item 4b)
- [x] `PipelineStore` / `AppStore` / `HostStore` / `EnvironmentStore` `.List` take `(limit, offset)` with the RunStore contract: `limit <= 0` = unbounded (internal callers pass `0,0`), negative offset = 0, stable per-store `ORDER BY`.
- [x] Handlers accept `?limit=&offset=` (default **100**, max 1000) on GET `/pipelines`, `/apps`, `/hosts`, `/environments`; response shape unchanged.
- [x] Postgres uses `LIMIT NULL` for unbounded via shared `limitArg`/`clampOffset` helpers; memory stores slice after sort via a shared `paginate` helper.
- [x] Frontend `api/{pipelines,apps,hosts,environments}.list()` accept optional `{limit, offset}` (`pageQuery` in `client.ts`); `tsc`, build, and 80/80 vitest green.
- [x] Tests: store paging contract (pages, past-end, negative offset) + handler default-100/offset test.

## AC-7.4 — Retention + resource quick-wins (O4, scan items 2 & 4a + report item 7 sub-point)
- [x] `jobqueue.Store.DeleteOlderThan(cutoff)` (postgres + memory) deletes **terminal** jobs only — pending/running are never swept; contract test covers all four states.
- [x] Daily sweeper in `server.New` gated by `COOKER_JOBQUEUE_RETENTION` (default `720h`, `0` disables), boot-sweep first, same drain pattern as the audit sweeper.
- [x] Compose resource ceilings: `mem_limit`/`cpus` on cooker (1g/1.0), postgres (512m/1.0), redis (128m/0.2), traefik (128m/0.5); all three compose files still `config`-render.
- [x] FULL-edition build cache documented: `COOKER_BUILD_CACHE_REPO` example in `full.env.example` + commented `extraEnv` entry in `values-full.yaml`.
- [x] Helm lookup footgun: WARNING blocks in `secret-key.yaml` / `postgres.yaml` + INSTALL.md callout (template/`--dry-run` renders mint fresh secrets → GitOps must use `existingSecret`).
- [x] Scan-report remediation list updated with LANDED markers for items 1–4.

## AC-7 — Cross-cutting
- [x] Every item is its own commit on `claude/optimization-round-wwh912`; local gates (gofmt, build, vet, full `-race` suite, frontend tsc/build/test, compose render, doc-links) green before push.
- [ ] Live-load verification (429 under a real run burst; TOAST WAL reduction measured; page-through on a >100-row install) — needs a live stack (reviewer/UAT).

