# Full-scan technical report — performance & vulnerability, by file type

**Date:** 2026-07-12 · **Scope:** the whole repo at merge-state after round-2 (editions + env/proxy) · **Mode:** report only — **no fixes applied in this PR.**

This report scans the codebase per file type and records performance + vulnerability findings with severity and file:line evidence. It cross-checks the existing audit series in `docs/audits/*` and does **not** re-flag items those docs mark closed. A ranked remediation order is at the end.

## Method & tool status

| Pass | Tool | Status | Result captured |
|---|---|---|---|
| Go lint | `golangci-lint run ./...` | ✅ ran | 32 issues (13 unused, 13 errcheck, 6 staticcheck) |
| Go SAST | `gosec ./...` | ✅ ran | 57 raw issues (see triage below — many false positives) |
| Go vuln (deps) | `govulncheck ./...` | ⚠️ **blocked** | `vuln.go.dev` returns 403 through the sandbox agent-proxy (policy denial). **Not run.** Recommend adding a `govulncheck` CI job — it is the authoritative Go CVE gate and can't be substituted by grep. |
| JS/TS deps | `npm audit` | ✅ ran | 5 vulns: 1 critical, 1 high, 3 moderate (all dev-only, all fix-available) |
| Secret scan | `git ls-files | grep -E <key/token/pw patterns>` | ✅ ran | **0 real hits.** The one `BEGIN PRIVATE KEY` match (`docs/guides/UAT.md:237`) is an elided `…` doc placeholder. No AWS `AKIA` keys. |
| SQL / Go perf | manual agent review vs. `docs/audits/*` | ✅ ran | 9 findings (3 tracked-open, 6 new) |
| Infra (YAML/Dockerfile/shell/TF) | manual agent review vs. SECURITY.md + audits | ✅ ran | 9 new findings (4 Medium, 5 Low) |

**Not available in this environment** (documented so the gaps are explicit, not silently skipped): `govulncheck` (proxy-blocked), `trivy`/`hadolint` (Dockerfile+image CVE), `semgrep`, `shellcheck`, `tfsec`/`checkov`. The manual reviews cover those dimensions with explicit checklists; the durable fix is to add the tool jobs to CI.

**Headline:** no unaccepted **Critical/High** defect in first-party code. The highest real risk is a **High performance** item (unbounded run-spawn concurrency, already tracked open). The most notable *new* correctness risk is a **Medium** Helm secret-regeneration footgun introduced by round-2 chart work.

---

## Go (`*.go` — 426 files)

### Vulnerability (gosec, triaged)

gosec reported 57 issues, but the HIGH bucket is dominated by **false positives** — recorded here honestly rather than inflating the count:

| gosec rule | Location(s) | Verdict |
|---|---|---|
| G704 "SSRF" ×4 | `cmd/cookerctl/client.go:93,110,176,184` | **False positive.** `cookerctl` is a CLI that calls its own operator-configured `baseURL`. Talking to a user-named server is the tool's purpose, not SSRF. |
| G703 "path traversal" | `cmd/cookerctl/commands.go:250` | **False positive.** CLI reads an operator-specified local file (pipeline YAML to import). |
| G404 weak RNG | `internal/server/wshub_backend.go:33` | **False positive.** It's the `math/rand` *fallback* used only if `crypto/rand.Read` fails, for backoff jitter (non-security). |
| G115 int overflow ×3 | `wshub_backend.go:277`, `deploy/deploytarget/ecs/ecs.go:99,125`, `cloudrun/cloudrun.go:118` | **Guarded / benign.** `wshub_backend.go:277` is explicitly length-checked (`> int(^uint16(0))`) immediately before the cast; the ECS/CloudRun ones are small config counts (replica/port) that cannot realistically overflow int32. |

| severity | file:line | category | finding |
|---|---|---|---|
| Low | `internal/server/*` (G112 ×2) | vulnerability | Two HTTP servers without `ReadHeaderTimeout` (Slowloris) — **verify** these are the metrics/main listeners; the main server does set timeouts, so confirm which servers gosec flagged and add `ReadHeaderTimeout` if any lack it. |
| Low | `internal/*` (G306/G302/G301 ×6) | hardening | A handful of `os.WriteFile`/`MkdirAll` use `0644`/`0755` where `0600`/`0750` would be tighter (temp workdirs, kubeconfig writes). Low impact (pod-local ephemeral dirs) but worth tightening on secret-bearing writes. |
| Low | `internal/*` (G401/G505 sha1 ×2) | vulnerability | `crypto/sha1` import + use — **verify** it's for a non-security digest (cache key / ETag), not signing. If digest-only, annotate with a `#nosec` + comment; if security-relevant, migrate to sha256. |
| Low | `internal/*` (G204 ×15) | (info) | "subprocess with variable" — inherent to the docker/kubectl/git/helm adapters, which pass argv (not shell strings). Argv form = no shell injection; these are expected. No action beyond keeping argv (never `sh -c`) form. |

### Quality (golangci-lint — 32)
- **13 unused** — mostly dead code: `store/postgres/store.go:280-311` (`errMigrationNotApplied`, `rollbackMigration`, `loadAppliedMigrations` — the never-wired down-migration harness the Phase-1 review flagged), plus unused test helpers. (`ProxyConfig.urlFor` from round-2 was already removed.)
- **13 errcheck** — unchecked `Close()`/`Fprint` in `deploytarget/ssh/ssh.go` (7), `store/postgres/store.go` (2), `cmd/cookerctl` (4). Mostly deferred-Close on read paths; low risk, but the ssh-session closes are worth checking.
- **6 staticcheck** — comment-form nits (ST1021/ST1022), one `SA9003` **empty branch** in `source/github/clone.go:88` (worth a look — an empty `if` can hide a dropped case), a tagged-switch suggestion, and two test var-naming (ST1003).

### Performance (manual review vs. `docs/audits/*`; closed items excluded)

| severity | file:line | category | finding |
|---|---|---|---|
| **High** | `server/runs.go:75-131` | performance | `RunCoordinator.Spawn`/`SpawnWithDeadline` have **no global concurrency cap** — every `POST /runs` and `POST /apps/:id/deploy` spawns a heavy build/deploy goroutine; a burst risks FD/registry-rate/OOM exhaustion. *(tracked open in dag-performance.md; only the per-run deadline landed.)* |
| Medium | `store/postgres/run.go:78-82` | performance | `RunStore.Get` returns raw `stage_runs` incl. per-stage logs (1 MiB/stage cap) — polled `GetPipelineRun` can emit a ~50 MiB body for a 50-stage run. `List` already strips logs via `jsonb_agg(elem - 'logs')`; `Get` should too. *(tracked open.)* |
| Medium | `store/postgres/run.go:146-174` | performance | `RunStore.Update` re-marshals all three JSONB blobs (logs-bearing `stage_runs` + env + variables) on **every** progress flush, though only `stage_runs` changes — steady write amplification proportional to log volume. *(tracked open.)* |
| Medium | `handler/{app,pipeline,host,environment}.go` List → `store/postgres/*.go List()` | performance | Apps/pipelines/hosts/environments list endpoints return the **entire table** with no `LIMIT`/pagination (runs/deploys/audit are all capped). Full re-scan + marshal per request as data accumulates. *(new)* |
| Low | `store/memory` package `RWMutex` | performance | Coarse package-level mutex serializes unrelated ops — dev/in-memory backend only; noted for completeness. *(new)* |

**Verified clean** (no finding — cross-checked and excluded): every outbound `http.Client` in `internal/` sets a `Timeout` or context deadline; promotion/stage-approval lists use batched `ANY($1)` (no N+1); jobqueue pool is capped with `FOR UPDATE SKIP LOCKED`; the audit-series DA-H2/H3 batching, F3/F10/F18 status paths, canary-sweeper CAS, and cloud-cache herd fixes all remain in place.

---

## SQL (`*.sql` — 52 migrations + store queries)

| severity | file:line | category | finding |
|---|---|---|---|
| Medium | `migrations/010_jobs.up.sql` + `jobqueue/postgres.go:278` | performance | `jobs` table has **no retention/DELETE** (unlike `audit_events.DeleteOlderThan`); terminal rows + `jobs_created_idx` grow unbounded, and `Stats()` does a full-table `GROUP BY status`. *(new)* |
| Low | `migrations/015_pipeline_run_actor.up.sql:10-14` | correctness | Four `ADD COLUMN` **without `IF NOT EXISTS`**, unlike sibling migrations (006/007/008/009/017/025) — non-idempotent; a partial-apply re-run aborts. *(new)* |
| Low | `store/postgres/audit_event.go:57-65` + migration 019 | performance | `method =` / `path LIKE 'prefix%'` audit filters have no supporting index (only `(time DESC)` and `(user_sub, time DESC)`) — seq-scan when filtering without a narrow time window. *(new)* |
| Low | `migrations/020_run_promotions.up.sql:37`, `022_stage_approvals.up.sql:39` | performance | `idx_*_run (run_id)` is redundant with the leading column of the `UNIQUE (run_id, …)` constraint's btree — dead write cost. *(new)* |

**Verified clean:** all `ALTER ... ADD COLUMN` on hot tables (006/008/009/015/017/025) use constant `DEFAULT`s (metadata-only on PG11+), so no lock-hazard table rewrite. Migrations run under `pg_advisory_lock` (Phase-0 review).

---

## YAML — compose / Helm / k8s / CI (`*.yaml`,`*.yml` — 53 files)

| severity | file:line | category | finding |
|---|---|---|---|
| Medium | `deploy/aws/values/values-aws-{starter:41,team:38,scale:31}.yaml` | hardening | Kaniko executor overridden to mutable `:latest` in all three prod-tier overlays (chart pins `v1.23.2`) — supply-chain path into the privileged in-cluster build executor with ECR-push IAM. *(new)* |
| Medium | `.github/workflows/ci.yml`, `oci-conformance.yml` | hardening | No top-level `permissions:` block (unlike `release.yml`/`cooker-weekly.yml`), so `GITHUB_TOKEN` inherits the repo default (often write-all) while running untrusted PR code. *(new)* |
| Medium | `deploy/helm/cooker/templates/secret-key.yaml:17-22`, `postgres.yaml:16-22` | vulnerability | **Round-2 footgun:** the `lookup`+`randAlphaNum` persistence trick returns empty under `helm template`/`--dry-run`, so a GitOps `helm template | kubectl apply` pipeline **regenerates `COOKER_SECRET_KEY`** (→ all sealed env secrets undecryptable) and the bundled-PG password (→ PVC/password mismatch) on every render. *(new — introduced by this effort's chart work)* |
| Low | `docker-compose.prod.yml`, `docker-compose.full.yml` | hardening | No CPU/memory limits on any production compose service — a runaway workload can starve the single host. *(new)* |
| Low | `deploy/kubernetes/postgres.yaml:84-93`, `helm .../postgres.yaml:79-88` | hardening | Bundled-Postgres container has no container-level `securityContext` (caps/PE/roFS), unlike its `cooker`/`redis` siblings. *(new)* |

**Verified clean / accepted (not re-flagged):** UAT `docker.sock` + k3s `privileged` (documented, socket-proxy alternative), the proxy overlay's read-only socket, chart NetworkPolicy egress deny-with-exceptions, namespace-scoped builder RBAC, `optional:false` secretKeyRefs, ingress-TLS fail-guard, S26-05-13 no-default-PG-password. Workflow action pinning is complete (SHA-pinned).

---

## Dockerfile (`deploy/docker/*`)

| severity | file:line | category | finding |
|---|---|---|---|
| Low | *(missing)* `/.dockerignore` | hardening | No `.dockerignore`; build context is repo root (`docker-compose.prod.yml:97`), so `.git`, `.env.prod`, and any `frontend/.env*` are sent to the daemon and can bake into layers. *(new)* |
| Low | `deploy/docker/Dockerfile.backend:1,3`, `Dockerfile.frontend:1` | hardening | Orphaned dev Dockerfiles (documented as unreferenced) use unpinned base + `go install …air@latest`. Dead files; a stray `docker build -f` pulls unpinned code. *(new)* |

The real `deploy/docker/Dockerfile` is well-hardened (multi-stage, non-root UID 65532, SHA-pinned kubectl, HEALTHCHECK, pre-gzipped assets). `trivy`/`hadolint` unavailable — recommend a CI image-scan job.

---

## Shell (`*.sh` — 13)

No new findings. Scripts under `scripts/` and `.claude/skills/` use `set -euo pipefail` and quoted expansions; `make`-driven secret generation reads `/dev/urandom` and writes to `.env.*` (git-ignored). `shellcheck` unavailable — recommend adding it to CI for ongoing coverage.

---

## Terraform (`deploy/aws/terraform/` — 8 `.tf` + 3 `.tfvars`)

| severity | file:line | category | finding |
|---|---|---|---|
| Medium | `modules/cluster/main.tf:27-50` | hardening | EKS control-plane endpoint left at the module default (public, `0.0.0.0/0`) — no `cluster_endpoint_public_access(_cidrs)` set. IAM-gated but a broad internet-reachable API surface for a "review before apply" skeleton. *(new)* |
| Low | `modules/registry/dockerconfig-refresh.example.yaml:84` | hardening | `amazon/aws-cli:latest` mutable tag in an example CronJob operators apply out-of-band. *(new)* |

**Verified clean:** RDS encrypted + non-public + deletion-protected; IAM least-privilege (the only `resources=["*"]` is `ecr:GetAuthorizationToken`, which cannot be resource-narrowed and is commented as such).

---

## JavaScript / TypeScript (`*.ts`,`*.tsx`,`*.js` — 84 + 21)

`npm audit`: **5 vulnerabilities, all in dev-only tooling, all with fixes available** — none ship in the production bundle (Vite/Vitest/esbuild are build/test-time):

| severity | package | via | fix |
|---|---|---|---|
| Critical | `vitest` (≤3.2.5) | UI server arbitrary file read/exec | `npm audit fix` (major bump) |
| High | `vite` (≤6.4.2) | path traversal in optimized-deps `.map`; launch-editor NTLM disclosure (Windows) | `npm audit fix` |
| Moderate | `esbuild` (≤0.24.2) | dev-server request/response leak | via vite bump |
| Moderate | `@vitest/mocker`, `vite-node` | transitive via vite/vitest | via bump |

`tsc --noEmit`, `eslint` (0 errors, 4 pre-existing `react-refresh` warnings in `OIDCProvider`), and `vite build` are green. The frontend is currently page-stubs (Phase 2), so the runtime attack surface is minimal; the dev-tool CVEs are still worth clearing on the next `npm` bump.

---

## Recommended remediation order (for a future fix round — nothing applied here)

1. **[High, perf] Global run-concurrency cap** — `server/runs.go` `RunCoordinator`: bound in-flight spawns (worker pool or semaphore) so deploy/run bursts can't exhaust the host. Highest blast-radius, already tracked.
2. **[Medium, security] Helm secret-regen footgun** — `secret-key.yaml` / `postgres.yaml`: document loudly that template/`--dry-run` workflows must supply `existingSecret`/`secretKey.value`, or gate the `randAlphaNum` path behind an explicit `.Values.*.autogenerate` that GitOps sets false. (Introduced by round-2; fix alongside it.)
3. **[Medium, perf] Run JSONB read/write bloat** — strip logs from `RunStore.Get` (mirror `List`); make `RunStore.Update` write only `stage_runs` on progress flush.
4. **[Medium, perf] jobs-table retention** + **[Medium, perf] list-endpoint pagination** (apps/pipelines/hosts/envs).
5. **[Medium, hardening] Pin the AWS-overlay Kaniko executor** to the chart's digest; add `permissions:` to `ci.yml`/`oci-conformance.yml`; set EKS endpoint access explicitly.
6. **[CI, coverage] Add the tool jobs that couldn't run here:** `govulncheck` (proxy-blocked), `trivy` image scan, `shellcheck`, `hadolint`, and `.dockerignore`. This turns one-off gaps into standing gates.
7. **[Low] Cleanup sweep** — golangci `unused`/`errcheck`/`staticcheck` (delete dead down-migration harness, check the empty `if` in `clone.go:88`, close ssh sessions); tighten `0644→0600` on secret-bearing writes; drop redundant `idx_*_run` indexes; add `IF NOT EXISTS` to migration 015; add compose resource limits + postgres container securityContext.

Nothing in this report was fixed in this PR (per scope). Each item carries enough evidence (file:line, tool) to action in a targeted follow-up.
