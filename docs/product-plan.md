# Cooker — Product Plan: Roadmap, Production Path, Monetization

> **Status:** advisory, written 2026-06-10 for a solo-maintainer, portfolio/open-source project. This document recommends; the authoritative engineering state lives in [`backlog.md`](../backlog.md) and [`docs/audits/launch-readiness.md`](audits/launch-readiness.md). Every claim below cites a backlog/audit ID — see the [Appendix](#appendix-finding-id-index) for the index. Cost figures are rough 2026-06 estimates, not quotes.

---

## 1. Executive summary

Cooker is a graph-based CI/CD + self-hosted-PaaS hybrid: a single Go binary that serves a visual pipeline editor, builds OCI images in-cluster, pushes to registries, and deploys to Kubernetes and five other target types — with OIDC/RBAC, audit logging, scheduling, notifications, and pluggable secrets backends already shipped.

**The honest verdict, in one paragraph:** the *security and stability* work is done — all five remediation phases (T1–T24) landed, the SPOF closeout landed, and [`launch-readiness.md`](audits/launch-readiness.md) says "ship to UAT" with zero launch-blocking chains. What is **not** done is *functional completeness of the advertised surface*: the environment promotion/approval flow is decorative (`HS26-05-01`), the GitHub-webhook auto-deploy loop ends in a TODO (`HS26-05-02`), Settings registry/cluster CRUD persists nothing (`HS26-05-04`), and three pipeline stage types (Test / Approval / Custom) are unimplemented — they now fail loudly rather than fake success, but they don't run (`HS26-05-03`). **"Production-grade" for this project means closing those functional gaps, not more hardening.**

Recommended sequence:

1. Close the fix-first list (§3) — roughly **one focused week** of work.
2. Stand up a hosted UAT on a small VPS with OIDC on and Kaniko (§6.2) — **~$6–12/mo**.
3. Go production as **single-replica k3s-on-VPS** per the backlog's "Ship it" shape (§6.3) — **~$15–45/mo**.
4. Grow adoption with the Tier 1/2 roadmap (§5); treat monetization as a ladder that starts with adoption, not revenue (§7).

---

## 2. Current-state assessment

### 2.1 What's genuinely done

Per [`launch-readiness.md`](audits/launch-readiness.md) TL;DR (all ✅ Landed):

| Area | Status |
|---|---|
| Phase 0 security hot-fixes (Buildah injection [A8-1], GitOps traversal [A8-3], IDOR [A6-1/2], prod validation, RBAC scoping) | ✅ Closed |
| Phase 1 stability (panic recovery, races, leaks, body/memory limits, WS deadlines) | ✅ Closed |
| Phase 2 reliability (per-stage timeout+retry, optimistic concurrency, idempotency keys, log persistence) | ✅ Closed |
| Phase 3 production hardening (migrations, async audit sink, HPA/PDB, trace propagation) | ✅ Closed |
| Phase 4 polish (CSP, /version, runbook) + W1–W5 launch prep | ✅ Closed |
| Boot resilience (Postgres backoff, lazy OIDC discovery, SIGTERM drain, orphan sweep) | ✅ PR #21 |
| Chain coverage | 19/54 closed; **0 launch-blockers** in the remaining 28 |

Feature surface already shipped: visual DAG editor; Docker/Kaniko/Buildah builders (+ BuildKit in code, not yet chart-wired); Docker/Crane pushers; kubectl/client-go deployers; Kubernetes/Cloud Run/ECS/Fly.io/Render/SSH-Docker/Compose deploy targets; Apps (single-repo deploy with webhook auto-deploy intent, rollback, drift detection); five secrets backends (DB AES-GCM, Vault, AWS, GCP, KeepSave); OIDC PKCE + 4-role RBAC + MFA step-up + optional local auth; leader-elected cron scheduler; Slack/Discord/Email/Webhook notifications; pipeline templates; durable Postgres job queue + run FSM; `/metrics` + OTel tracing; queryable audit trail; opt-in AI failure triage; run diff + stage-duration analytics; ~20 frontend pages.

### 2.2 What's functionally broken (verified live, 2026-06-10)

These are the gaps a real user hits in the first hour. IDs from [`2026-05-half-shipped.md`](audits/2026-05-half-shipped.md); each re-verified against current code before listing here.

| ID | What a user sees | Reality in code |
|---|---|---|
| `HS26-05-01` (HIGH) | "Promote to Staging" / "Approve" buttons appear to work | `handler/environment.go:259-308` returns `"promotion initiated"` and persists nothing; `GetEnvStatus` always returns `[]`; `RequiredApprovers` never enforced; no promotion migration exists (only 001–019). Pairs with `HS26-05-08` and `HS26-05-14`. |
| `HS26-05-02` (HIGH) | GitHub webhook configured, push lands, 202 returned, "auto-deploy" pill on | `handler/app.go:376` is `// TODO: enqueue a real deploy` — no run is created. |
| `HS26-05-04` (MEDIUM, first-impression killer) | Settings → add registry/cluster → "registry added" toast | `handler/registry.go:110` returns a canned 201; next list paint is empty. Every operator persona hits this on first login. |
| `HS26-05-03` (HIGH, re-graded) | Test / Approval / Custom stages in the palette | Unimplemented at runtime. Since the W3 fix they **fail loudly** (`executor.go` returns `stage type %q not implemented`) instead of silently succeeding — a trust fix, but the features still don't exist. |
| `HS26-05-05` (MEDIUM, partial) | Kubernetes page | Read-only list/inspect is real since PR #100; the write actions (scale/restart/apply/delete) remain stubbed. |
| W6.2 (HIGH, ~1 h) | Live logs during a WebSocket reconnect | Lines emitted in the 0–30 s reconnect window are permanently dropped (`frontend/src/hooks/useWebSocket.ts`). |

> Other `HS26-05-*` findings predate the W3–W5 closeouts; treat that audit as "as of 2026-05" and spot-check before quoting. The six above were re-verified for this document.

### 2.3 Notable absences (not bugs — features that don't exist)

- **API tokens / service accounts** — no way to call the API from a script or external CI; OIDC browser flow only.
- **CLI** — all interaction is web UI or raw HTTP.
- **Pipeline-as-code** — no YAML import/export; pipelines live only as DB JSONB.
- **Git-push → pipeline triggers** — webhooks only drive Apps auto-deploy; pipelines run manually or by cron.
- **Multi-tenancy / teams / per-team RBAC** — single tenant; all users see all resources; roles are instance-wide.
- **SAML/LDAP** (OIDC only), **canary/blue-green deploys**, **image vulnerability scanning / SBOM**, **test-report parsing** (artifact type exists, never populated), **secret diff view between environments**, **user management UI**, **PR-preview environments**.

§5 prioritizes these for OSS adoption; most are deliberately deferred.

---

## 3. Fix-first: the production-grade gate

Security hardening is done; this list is **functional completeness**, ordered by leverage. Items 1–2 are the true gate — everything after is polish.

### Code fixes (sequence)

| # | Item | Effort | Notes |
|---|---|---|---|
| 1 | **`HS26-05-01` — real promotion + approval persistence** | ~1–2 days | Needs a mini-ADR first (approval rows vs. column on `pipeline_runs`), migration `020_promotion_approval`, fix `handler/environment.go:259-308`, enforce `RequiredApprovers`, fold in the `HS26-05-14` `approvedBy` body-shape fix. Anchors the whole Dev→Staging→Prod story. |
| 2 | **`HS26-05-02` — webhook → real deploy** | ~½ day | Replace the `app.go:376` TODO with the existing synthesized Clone→Build→Push→Deploy path (`AppDeployer.DeployImage`). Closes the "push and it deploys" demo loop. |
| 3 | **`HS26-05-04` — Settings registry/cluster persistence** | ~½ day | Mirror the `HostStore` pattern for `RegistryConfigStore`/`ClusterConfigStore`. The audit's own quick-win #1. |
| 4 | **`HS26-05-03` — implement the three stage runtimes** | ~½ day each | Test runner, approval gate (depends on #1's store), custom script runner. Fail-loud today, so lower urgency than 1–3. |
| 5 | **W6.2 — `useStageLogs` reconnect backfill** | ~1 h | Re-issue `getStageLogs` on each reconnect; last open HIGH defect. |
| 6 | **Honest-501 sweep** | ~1 h | Remaining `docker.go` / `kubernetes.go` write stubs the UI paints green (`HS26-05-05`, `HS26-05-15`): return 501 so toasts are honest until implemented. |

### Operator steps (parallel, no code)

- **P1.5** — enable Renovate/Dependabot in the GitHub UI (`renovate.json` already shipped).
- **S26-05-15** — pin the 17 floating GitHub Actions to SHAs; do `anthropics/claude-code-action@v1` in `cooker-weekly.yml` **first** (it runs with `contents: write` + `pull-requests: write`).
- **W6.3** — capture the three README screenshots (`docs/images/`) — also Tier 1 of §5.
- **W6.4** — delete ~80 stale merged branches (command in [`backlog.md`](../backlog.md) §W6.4) or enable auto-delete-head-branches.

---

## 4. Production-grade checklist

Do **not** maintain a second checklist here — [`docs/audits/launch-readiness.md`](audits/launch-readiness.md) is the single source of truth and is already operator-facing. In short it walks:

1. **Configuration** — `COOKER_ENV`, real `DATABASE_URL` (dev default rejected in production), `COOKER_SECRET_KEY` (base64 32-byte), OIDC vars, exact `COOKER_ALLOWED_ORIGINS` (wildcard rejected), Redis backends when `replicaCount > 1`.
2. **Helm-chart settings** — `secretKey.existingSecret`, `builder.kaniko.contextPVC` (chart fails fast without it), HPA/PDB when multi-replica, probe tuning.
3. **RBAC + network** — NetworkPolicy on, TLS ingress in front (chart refuses `production + oidc + ingress` without `ingress.tls`).
4. **Smoke tests** — `/health/live`, `/health/ready` (db/redis checks), `/version`, OIDC round-trip, WS log attach, idempotency replay, optimistic-concurrency 409.
5. **Observability** — Prometheus scraping `/metrics`, optional OTel, audit-sink drop alerts per [`RUNBOOK.md`](guides/RUNBOOK.md).
6. **Backup + restore** — Postgres is the only stateful component; rehearse restore before launch.

The cutover procedure itself (UAT → production order of operations) is [`docs/guides/ROLLOUT.md`](guides/ROLLOUT.md).

---

## 5. Feature roadmap for open-source adoption

**Adoption thesis:** people adopt a self-hosted CI/CD tool when (a) the headline loop works end-to-end on first try, (b) it's automatable — tokens, YAML, CLI, and (c) it reacts to `git push`. Multi-tenancy and SAML are enterprise-sales features; they do nothing for a star count and are explicitly deferred.

### Tier 1 — makes people try it and keep it

| Item | Effort | Why |
|---|---|---|
| Working promotion/approval (= §3 #1) | 1–2 d | It's the differentiating headline feature; today it's theatre. |
| Working webhook deploy (= §3 #2) | ½ d | "Push and it deploys" is the demo. |
| README screenshots + a 90-second GIF (W6.3) | ½ d | Half the star decision happens on the README. |
| **API tokens / service accounts** | 1–2 d | Migration + auth middleware accepting a token alongside the Bearer JWT. Gateway to CLI, scripting, external CI. |

### Tier 2 — makes people adopt it for real work

| Item | Effort | Why |
|---|---|---|
| Pipeline-as-code: YAML import/export | 3–5 d | The #1 expectation for any CI/CD tool; the DAG already serializes to JSON — add a YAML round-trip + `POST /pipelines/import`. |
| Git-push → pipeline triggers | 2 d | Extend the existing webhook receivers (GitHub/GitLab/Bitbucket/Gitea) from Apps to Pipelines. |
| CLI (`cooker` binary over API + tokens) | 3–5 d | Unlocks scripting and CI-of-CI; depends on Tier 1 tokens. |
| First-run empty-state CTAs (W11 P2) | 1 d | Softens the onboarding cliff on Apps/Pipelines/Environments. |

### Tier 3 — deliberately deferred

Multi-tenancy / per-team RBAC (needs an ADR; enterprise-only value), SAML, canary/blue-green, image scanning/SBOM, test-report parsing, user-management UI, PR-preview environments, native Docker SDK surface (blocked on Go 1.26, P9.4). Revisit only with real user demand or a commercial trigger (§7).

---

## 6. Deployment runbook: UAT first, then production

### 6.1 What `make uat-up` gives you today (local UAT)

Per [`docs/guides/UAT.md`](guides/UAT.md): first run generates `.env.uat` with a fresh `COOKER_SECRET_KEY` and auto-detected `DOCKER_GID`, then `docker compose -f docker-compose.uat.yml up -d --build` starts **cooker** (host docker.sock + read-only kubeconfig), **postgres**, a CNCF **registry**, a single-node **k3s**, and a kubeconfig-fixer. Auth is **off by design** (dev-admin injected); `COOKER_ENV=uat` keeps CORS/validation lenient. Variants: `make uat-up-with-keycloak` (real OIDC, seeded `alice`/`bob`) and `make uat-up-socketproxy` (drops direct docker.sock).

This is a *local* acceptance environment. For a hosted UAT that rehearses production:

### 6.2 Hosted UAT — recommended target

- **Infra:** one small VPS — 2 vCPU / 4 GB (Hetzner CX22 ≈ €4/mo, or a $12–24 DigitalOcean droplet). **~$6–12/mo typical.**
- **Stack:** the UAT compose stack as-is, **plus**:
  - **OIDC ON** via `.env.uat` — use the Keycloak preset (`make uat-up-with-keycloak`) or a free Google OAuth client (presets documented in `.env.uat.example`). Never hardcode `COOKER_OIDC_ENABLED=true` into the compose file — that's a deliberate repo rule.
  - **Builder = kaniko** against the bundled k3s, to rehearse the production posture early (`Config.Validate` refuses the docker builder in production anyway).
- **Purpose:** walk the [`launch-readiness.md`](audits/launch-readiness.md) Pre-UAT checklist §1–§5 here, including the OIDC round-trip and WS log smoke tests, before touching production.

### 6.3 Production — recommended target (and two alternatives)

Backlog's deployment-shape matrix row 1 is the documented "✅ Production-ready. Ship it." shape: **single-replica + TLS ingress + Kaniko + Postgres SSL + edge rate limit**. For a solo portfolio project that is the right starting point — no Redis required, lowest cost, fully validated at boot.

| Option | Shape | Rough cost / mo | Verdict |
|---|---|---|---|
| **Recommended: k3s on one VPS** (4 vCPU / 8 GB, e.g. Hetzner CPX31/CX32) + cert-manager (Let's Encrypt) + Helm chart, single replica | Matrix row 1 | **$15–45** — VPS $8–30; Postgres either co-hosted with nightly `pg_dump` off-box (cheap, fine at this scale) or managed (Neon/Supabase/DO, $0–15) | Best cost/ops balance; everything the chart needs (ingress, NetworkPolicy, PVC for Kaniko context) works on k3s. |
| DigitalOcean Managed Kubernetes (DOKS) | Row 1 or row 3 (HA) | **$50–75** — node(s) $24+, LB $12, managed PG $15 | Less ops burden, real cloud LB; pick this if you'd rather not own a node. |
| EKS / GKE | Row 3 | **$150+** — EKS control plane alone ≈ $73 | **Not recommended** at portfolio scope — pure overhead. |

Scale-up path: when one replica isn't enough, move to matrix row 3 (multi-replica + Redis-backed hub/tickets/rate-limit — the chart's *default* values shape) per [`docs/guides/MULTI_REPLICA.md`](guides/MULTI_REPLICA.md). Don't start there.

### 6.4 Configuration delta: UAT → production

Keys verified against `deploy/helm/cooker/values.yaml` and `.env.uat.example`:

| Setting | Hosted UAT (compose) | Production (Helm) |
|---|---|---|
| `COOKER_ENV` / `cookerEnv` | `uat` | `production` (chart default; gates `Config.Validate`) |
| OIDC | on, Keycloak/Google preset in `.env.uat` | on, real IdP; `oidc.clientSecretRef` → pre-created Secret |
| `COOKER_ALLOWED_ORIGINS` / `oidc.allowedOrigins` | `http://localhost:8080` | exact HTTPS hostname(s); wildcard rejected at boot |
| Builder | `kaniko` (rehearsal) or socket-proxy variant | `builder.kind: kaniko` + `builder.kaniko.contextPVC` (required) + pinned executor image |
| TLS / ingress | none (IP or SSH tunnel) | `ingress.enabled: true` + `ingress.tls` (chart refuses prod+OIDC without it) |
| `COOKER_SECRET_KEY` | auto-generated into `.env.uat` | `secretKey.existingSecret` → pre-created Secret |
| Database | bundled compose Postgres | `database.host` + `database.passwordSecretRef` (chart fails closed without it) + `postgresql.sslMode: require` (or `verify-full`) |
| WS hub / tickets / rate limit | memory (single process) | memory is fine at `replicaCount: 1`; chart defaults to `redis` — keep redis if you may scale |
| HPA / PDB | off | off at 1 replica; both on when `replicaCount > 1` |
| Run retention | off | `retention.enabled: true` (CronJob, `daysToKeep: 90`) |
| Audit | `stdout` | `COOKER_AUDIT_DESTINATION=db,stdout` (queryable + loss-resistant) |
| NetworkPolicy / securityContext | n/a | both on (chart defaults; UID 65532) |
| K8s RBAC scope | n/a | consider `rbac.clusterWide: false` to confine pod-log reads to deploy-target namespaces ([SECURITY.md](../SECURITY.md)) |

**Order of operations for the cutover itself:** follow [`docs/guides/ROLLOUT.md`](guides/ROLLOUT.md) (shipped in PR #21) — it is the single source of truth and cross-references [`RUNBOOK.md`](guides/RUNBOOK.md) and [`MULTI_REPLICA.md`](guides/MULTI_REPLICA.md).

---

## 7. Monetization strategy (portfolio / open-source framing)

### Competitive reality

CI/CD is one of the most crowded categories in dev tooling: GitHub Actions and GitLab CI are free-by-default incumbents; Jenkins owns legacy; Harness/Buildkite/Codefresh own enterprise budgets; Argo and Tekton own the Kubernetes-native crowd. A solo OSS project does not win by feature parity. It wins a **niche**: Cooker's defensible position is *visual graph CI/CD + Coolify/Dokploy-style self-hosted PaaS in one Go binary*. The self-hosted-PaaS niche has proven star traction (Coolify, Dokploy, CapRover); none of them have a real pipeline DAG editor. That hybrid is the story to tell.

### The ladder (in order — each rung funds the next)

1. **Adoption first** — for a portfolio project, the realistic ROI is reputation and reach, which later converts to anything else. Concretely: README screenshots + 90-second GIF (W6.3); a live read-only demo instance (the UAT VPS can double as it); a sub-5-minute quickstart (`docker run` one-liner); launch posts on Show HN, r/selfhosted, r/devops; an honest "Cooker vs. Jenkins vs. Coolify" comparison page. Gate: don't launch until §3 items 1–2 are fixed — reviewers will find theatre features in minutes.
2. **Sponsorships** — `FUNDING.yml` (GitHub Sponsors / Open Collective) with modest tiers. Realistic early revenue is $0–200/mo; treat it as a signal, not income.
3. **Consulting / support** — "I'll deploy and operate Cooker for your team." Credible precisely because the runbooks (ROLLOUT/RUNBOOK/MULTI_REPLICA) already exist. This is the first rung that pays real money.
4. **Open-core — only after traction** (say, >1k stars and >10 known production deployments). Keep the core **Apache-2.0** — decide the license *now* and add a CLA if open-core is plausible later, because relicensing after outside contributions is painful. Natural paid-tier candidates are exactly the §5 Tier 3 deferrals: SAML, multi-tenant teams + per-team RBAC, compliance audit exports, signed-image policy enforcement, priority support. Alternative: a hosted control plane (SaaS) — but do not run a paid SaaS solo before §3 is closed and an external security review is done.
5. **Distribution** — DigitalOcean/Linode marketplace images, the Helm chart on Artifact Hub, an `awesome-selfhosted` listing. Free adoption channels that compound rung 1.

### Anti-goals

- No paywalls before traction — they kill the only asset (adoption) a portfolio project has.
- No enterprise features (multi-tenancy, SAML) built speculatively without a paying counterpart.
- No solo-operated paid SaaS on today's codebase — fix-first list and an external pen-test come first.

---

## Appendix: finding-ID index

| ID | Source |
|---|---|
| T1–T24, Phases 0–4 | [`docs/audits/remediation-plan.md`](audits/remediation-plan.md), status in [`launch-readiness.md`](audits/launch-readiness.md) |
| [A6-1] [A6-2] [A8-1] [A8-3] | [`docs/audits/vulnerabilities-and-chains.md`](audits/vulnerabilities-and-chains.md) — closed in Phase 0 |
| `HS26-05-01…17` | [`docs/audits/2026-05-half-shipped.md`](audits/2026-05-half-shipped.md) — six items re-verified 2026-06-10 (§2.2) |
| W6.2 / W6.3 / W6.4 | [`backlog.md`](../backlog.md) §W6 carry-forward |
| P1.5, P9.4 | [`backlog.md`](../backlog.md) §P1 / §P9 |
| S26-05-15 (Action pinning) | [`docs/audits/2026-05-action-pinning.md`](audits/2026-05-action-pinning.md) |
| W11 personas / P2 UX items | [`docs/audits/W11-user-journeys.md`](audits/W11-user-journeys.md) |
| Deployment-shape matrix | [`backlog.md`](../backlog.md) §Production readiness summary |
