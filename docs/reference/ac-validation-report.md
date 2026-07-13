# Acceptance-criteria validation report + quantitative change summary

**Date:** 2026-07-13 · **Validated tree:** `claude/ac-validation-wwh912` (= all rounds 1–2 work) · **Baseline for deltas:** `aeeb62e` (main before PR #150).

Method: every AC item in [`acceptance-criteria.md`](acceptance-criteria.md) was re-verified with **fresh evidence now** — commands re-run, files re-checked — not by trusting previously-checked boxes. Verdicts: **PASS** (evidence reproduced), **DEFERRED** (needs a live docker daemon / k8s cluster / browser this sandbox doesn't have), **BLOCKED** (external outage), **FAIL** (evidence contradicts).

## Verdict summary

| Verdict | Count | Share of 48 items |
|---|---|---|
| **PASS** | **37** | **77.1%** |
| DEFERRED (live-env only) | 9 | 18.8% |
| BLOCKED (CI outage, see below) | 1 | 2.1% |
| FAIL | 1 | 2.1% |

**Of the 39 items verifiable in this sandbox: 37 PASS / 1 BLOCKED / 1 FAIL → 94.9% pass.** The one FAIL is pre-existing lint debt, not a regression (below).

## Part A — validation matrix (evidence per item)

### AC-0 Backend review & tech-debt report — 3/3 PASS
All three docs exist and cross-link; every debt item carries file:line; each maps to a phase (`ls docs/reference/backend-review.md …`; content spot-checks).

### AC-1 Easy deploy — 4 PASS · 4 DEFERRED
| Item | Verdict | Evidence |
|---|---|---|
| `.env.prod` generated with fresh secrets, git-ignored | PASS | `make apply-edition-light` created it with random `POSTGRES_PASSWORD`/`COOKER_SECRET_KEY`/JWT key; `.gitignore:19` covers `.env.prod` |
| `cooker migrate up` applies / `down` refuses | PASS | compiled binary: `migrate down` → exit 2 + guidance; `migrate up` → starts `applyMigrations` (INFO log) without booting the server |
| No stale `//go:embed` SPA claim; `COOKER_STATIC_DIR` wired | PASS | 0 grep hits in docs; env read in `config.go`, consumed in `router.go` |
| `go build/vet` + changed-package tests | PASS | re-run: build+vet clean, 49 package suites `ok` |
| deploy-docker boot + `/health` 200 | DEFERRED | no docker daemon in sandbox; `docker compose config` renders all services |
| production `Validate()` boot posture | DEFERRED | config surface verified by unit tests; boot needs the daemon |
| run-persists-across-restart | DEFERRED | needs live stack |
| `deploy-k8s` quickstart pod Ready | DEFERRED | needs a cluster |

### AC-2 Frontend reset — 4/5 PASS · 1 DEFERRED
Design surface deleted (`design_handoff_cosmic_theme/`, `theme/`, `components/{ui,layout,pipeline}` all absent); plumbing intact (`api/client.ts`, `OIDCProvider.tsx`, `useWebSocket.ts`, stores present); 24 routes resolve in `App.tsx`; gates re-run green (`tsc --noEmit`, build, **80/80 vitest**, eslint 0 errors/4 pre-existing warnings). DEFERRED: interactive `npm run dev` auth-redirect + WS connect (needs browser; the plumbing is covered by the passing unit tests).

### AC-3a/3b/3c Backend restructure — 12/13 PASS · 1 DEFERRED
God-files split (fresh `wc -l` below); `store.New(nil,…×17)` callers: **0**; package-level route handlers (`ListDockerImages` etc. as bare funcs): **0**; `composeBaseDir` global + setter: **0**; `internal/{build,deploy,cloud,notify}/` all exist; `handler/service/store/model` untouched; `CLAUDE.md` + `.claude/` routing point at new paths; build/vet/test green after every move (re-verified at tip). DEFERRED: full UAT e2e (`make uat-up`) — needs docker daemon.

### AC-4 Editions — 2 PASS · 2 DEFERRED · 1 BLOCKED
| Item | Verdict | Evidence |
|---|---|---|
| Edition switch idempotent, secrets preserved | PASS | light→full→light: exactly 1 marker block, `COOKER_EDITION` flips, same secrets |
| INSTALL.md documents editions + in-place upgrade + tier-orthogonality | PASS | section present |
| light boots (no redis, memory backends, features off) | DEFERRED* | *render fully verified:* `compose config` → 3 services, `WS_HUB_BACKEND: memory`, no `REDIS_URL`; boot needs daemon |
| full boots (redis backends + features on) | DEFERRED* | render verified: 4 services incl. redis, `WS_HUB_BACKEND: redis`; full preset sets jobqueue/scheduler/metrics/tracing/audit-db |
| Helm light/full presets render via CI helm job | BLOCKED | presets YAML-parse locally; the CI helm job is part of the Actions outage below |

### AC-5 ENV injection + URL + proxy — 6/7 PASS · 1 DEFERRED
Named tests re-run green: `TestAppEnvResolver_MergePrecedence` (secrets>plain, stage wins), `_NilSafety`, `TestSynthesizePipeline_InjectsEnvAndIngress` (env block + Ingress + hostile-value quoting), `_NoProxyNoIngress`, `TestDeployedURLFor` (proxy/localhost/none table), `TestDockerRunArgs_LabelsAndNetwork`. Wiring greps: `EnvResolver` set in `server.New`; Ingress emitted only with domain; `AppDetailPage` renders `deployedURL`; proxy overlay `compose config` renders traefik + `cooker-proxy` network. DEFERRED: live Traefik routing of `<slug>.<domain>` (needs daemon).

### AC-6 Scan report — 4/4 PASS
Report exists with all 8 file-type sections + tool-status table; `golangci`(32)/`gosec`(57 triaged)/`npm audit`(5)/secret-grep(0) captured; `govulncheck` recorded as proxy-blocked; that PR's diff = report + AC doc only; ends with ranked remediation order.

### Cross-cutting — 2 PASS · 1 FAIL (+ CI outage noted)
- PASS: all phases landed as draft PRs (round-1 four merged green; round-2 three stacked); no secret/token/model-id in any artifact (fresh grep: 0).
- **FAIL: "`make lint` runs clean"** — `golangci-lint` exits 1 with 32 findings (13 unused / 13 errcheck / 6 staticcheck). **Pre-existing debt, not a round-1/2 regression** (the one new-code finding, `ProxyConfig.urlFor`, was already removed); CI's backend job gates `go vet`, not golangci. Cataloged in the full-scan report §Go/Quality with a cleanup item.
- **CI outage (affects the round-2 PRs #154–#156):** every check on all three PRs fails in 1–3 s with **no logs produced** (log download 404). The same docs/helm checks passed on identical content in round 1, and all gates pass locally — this is a GitHub Actions runner/quota/billing-level failure on the repo, not the code. **Action for the repo owner: check Settings → Actions (spending limit / runner availability) and re-run the checks.**

### Checkbox corrections applied
Three AC-1 boxes flipped `[ ]`→`[x]` (secrets-gen, migrate, static-dir — now evidence-verified); everything else already matched the evidence. No previously-checked box had to be revoked.

## Part B — what changed, in numbers (baseline `aeeb62e` → now)

### Repo totals
| Metric | Before | After | Δ | % |
|---|---|---|---|---|
| Files changed (whole effort) | — | 277 files | +5,218 / −21,120 lines | — |
| Total tracked LOC | 160,999 | 145,097 | −15,902 | **−9.9%** |
| Frontend `src/` LOC | 18,747 | 4,886 | −13,861 | **−73.9%** (design strip; plumbing kept) |
| Backend non-test Go LOC | 40,904 | 41,615 | +711 | **+1.7%** (features added while restructuring) |
| Deploy-surface files (`deploy/` + compose) | 60 | 71 | +11 | **+18.3%** |

### Maintainability (the God-file fixes)
| File | Before | After | % |
|---|---|---|---|
| `service/executor.go` | 1,520 | 774 | **−49.1%** |
| `store/memory/memory.go` | 963 | 30 | **−96.9%** (12 per-entity files) |
| `server/server.go` | 1,184 | 918 | **−22.5%** |
| `config/config.go` | 864 | 592 | **−31.5%** |
| Largest backend file overall | 1,520 | 961 | **−36.8%** |
| `store.New` positional args | 17 | 1 struct | **−94.1%** |
| Convention-breaking package-level route handlers | 21 | 0 | **−100%** |
| Handler-layer mutable globals | 1 | 0 | **−100%** |
| Package dirs under `internal/` | 55 | 55 | 0% count — 12 adapter packages relocated under 4 domain parents (findability, not churn) |

### Capability & deploy UX
| Metric | Before | After | Δ |
|---|---|---|---|
| One-command deploy paths | 0 | 6 (`deploy-docker{,-light,-full}`, `deploy-k8s{,-light,-full}`) + proxy overlay | **new** |
| Manual steps to first production boot | ~7 (provision PG, mint 3 secrets, origins, env wiring, compose) | 1 (`make deploy-docker-light`) | **−86%** |
| Env/secret injection into deployed apps | ✗ (CRUD only) | ✓ docker/ssh/k8s, precedence-tested | new |
| Deployed-app URL + reverse proxy | ✗ (dead scaffold) | ✓ DeployedURL + Traefik labels + k8s Ingress | new |
| Working `migrate` subcommand / configurable static dir | ✗ / ✗ | ✓ / ✓ | new |
| Editions (light/full) | ✗ | ✓ compose + Helm presets | new |

### Quality posture
| Metric | Value |
|---|---|
| AC pass rate | 37/48 overall (77.1%); **94.9% of sandbox-verifiable items** |
| Test gates | 49 Go package suites `ok`; 80/80 vitest; tsc + eslint 0 errors |
| Secret scan | 0 real hits |
| First-party Critical/High vulns (scan) | 0 unaccepted |
| Dependency CVEs | 5 (all dev-only tooling, all fix-available) |
| Known debt (cataloged, not hidden) | 32 golangci findings + full-scan remediation list |

### Reading the numbers
The repo got **9.9% smaller** while gaining six deploy paths and four runtime features — the shrink is the deliberate −73.9% frontend design strip; the backend grew only **+1.7%** despite the feature work because the restructure removed as much as the features added. The biggest maintainability wins are concentration-of-risk reductions: the worst file is 36.8% smaller, the worst constructor 94% narrower, and two whole classes of convention violation (package-level handlers, handler globals) are at zero.
