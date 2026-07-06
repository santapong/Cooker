# 00 — SRE, SLA/SLO & Reliability Readiness

**Domain:** Reliability, SRE, and SLA/SLO definition for launching Cooker as a real production service.
**Status:** assessment, written 2026-06-20. Advisory — no code was changed producing this doc.
**Grounding:** [`product-plan.md`](../product-plan.md), [`audits/launch-readiness.md`](../audits/launch-readiness.md), [`audits/2026-06-full-audit.md`](../audits/2026-06-full-audit.md), [`guides/RUNBOOK.md`](../guides/RUNBOOK.md), [`guides/ROLLOUT.md`](../guides/ROLLOUT.md), [`guides/MULTI_REPLICA.md`](../guides/MULTI_REPLICA.md), [`backlog.md`](../../backlog.md) "Production readiness summary". Metrics verified directly against `backend/internal/observability/observability.go`.

> **One-paragraph verdict.** The boot-path and crash-resilience work is genuinely done (Postgres backoff, lazy OIDC, SIGTERM drain, orphan sweep, multi-replica guards, six resilience metrics on `/metrics`). What is *not* ready for a **contractual** SLA is the operator scaffolding **around** those internals: there is **no committed-and-shipped Grafana dashboard or alert-rule bundle** (RUNBOOK points at `deploy/observability/dashboards/` which **does not exist on disk** — see [Gap O-1](#o-1-dashboards-referenced-but-absent)), **no status page**, **no automated backup/PITR** (it is operator-DIY by design), and **no on-call rotation, escalation contacts, or postmortem template instantiated**. A **self-hosted best-effort tier can launch today**; a **hosted-SaaS contractual SLA cannot** until §2–§4 below are closed. Effort to close the SLA-blocking gaps: ~**1 to 1.5 engineer-weeks** (see §5).

---

## 1. Proposed SLAs / SLOs

Two tiers. **Self-hosted** = operator runs their own cluster; Cooker the project offers *targets and tooling*, never a contractual promise. **Hosted-SaaS** = a Cooker-operated control plane (does not exist yet; this is the target to design against per `product-plan.md` §7 rung 4).

### 1.1 Availability & error-budget targets

| Tier | Availability SLO | Window | Error budget | Notes |
|---|---|---|---|---|
| Self-hosted (best-effort) | **99.0%** target, **no commitment** | 30-day rolling | 7h 18m/mo | Single-replica is a documented "✅ Ship it" shape (`backlog.md` matrix row 1) but is **not HA** — a pod restart blanks the UI. Quote 99.0% only with ≥ 2 replicas + PDB. |
| Hosted-SaaS v1 (contractual) | **99.5%** | 30-day rolling | 3h 39m/mo | Realistic first commitment for a small team. Requires HA (≥ 2 replicas, Redis backends, managed Postgres with PITR). |
| Hosted-SaaS aspirational (v2) | 99.9% | 30-day rolling | 43m/mo | Only after multi-AZ Postgres + multi-region read-path; do not promise at launch. |

**SLI definition for availability** = `1 − (rate(cooker_http_requests_total{code=~"5..", route!~"/health.*"}[30d]) / rate(cooker_http_requests_total{route!~"/health.*"}[30d]))`. Exclude `/health/*` (probe traffic) and 4xx (client error, not our budget). The `cooker_http_requests_total{method,route,status}` counter already supports this directly (verified `observability.go:39`).

### 1.2 Latency SLOs (key endpoints)

Backed by `cooker_http_request_duration_seconds{method,route}` (verified `observability.go:44`, default Prometheus buckets — note caveat below).

| Endpoint class | Route template | p95 objective | p99 objective |
|---|---|---|---|
| Read API | `GET /api/v1/pipelines`, `/runs/:id`, etc. | < 300 ms | < 800 ms |
| Mutating API | `POST /pipelines/:id/run`, `PATCH /pipelines/:id` | < 500 ms | < 1.5 s |
| Auth-gated boot | `/api/v1/*` cold (post JWKS fetch) | < 1 s | < 2 s |
| Health probes | `/health/ready` | < 1 s (it does a 1s DB ping + Redis ping + JWKS age) | < 1.5 s |

> **Caveat (Gap O-2):** `cooker_http_request_duration_seconds` uses `prometheus.DefBuckets` (top bucket 10s) — fine for API latency. But it is **not** the metric for build/deploy SLOs; those are long-tail and live on `cooker_pipeline_stage_duration_seconds` (custom buckets to 3600s, `observability.go:75`). Do not write a build-latency SLO against the HTTP histogram.

### 1.3 Pipeline build / deploy success-rate SLOs

These are the *product's actual job* and matter more than HTTP availability to a CI/CD user.

| SLI | Definition | SLO |
|---|---|---|
| **Build success rate** | `cooker_pipeline_stage_duration_seconds_count{type="build",status="success"} / …{type="build"}` | ≥ **95%** (excludes user-caused build failures — see caveat) |
| **Deploy success rate** | same, `type="deploy"` | ≥ **97%** (platform-side; user app crash-loops are out of scope) |
| **Stage p95 latency** | `histogram_quantile(0.95, …stage_duration…{type="build"})` | < 30 min (alert already drafted in RUNBOOK at 1800s) |

> **Caveat:** `status` here is the platform outcome (succ/fail), which conflates *user build errors* (their Dockerfile is broken) with *platform failures* (Kaniko Job evicted, registry push 5xx). For a credible success-rate SLO you must split these — today you cannot from this metric alone. **Recommend** adding a `failure_class` label (`user` vs `platform`) so the SLO measures platform reliability, not user mistakes. This is the single most important observability change for an honest build-success SLO. (S; see §5 OB-3.)

### 1.4 WebSocket log-stream reliability SLO

The live-log stream is the most visible reliability surface (a dropped stream looks like a broken build).

| SLI | SLO | Backing |
|---|---|---|
| WS attach success (ticket → upgrade → first frame) | ≥ 99% | No direct metric today — **gap**. `cooker_redis_connection_errors_total` is a proxy (a Redis blip silences broadcasts per `MULTI_REPLICA.md`). |
| Log-line delivery completeness | best-effort | `StageRun.Logs` persists on stage-finish so history is recoverable via `GET /runs/:id` even if the live stream drops (roadmap R5 / `useStageLogs` backfill). Don't SLO live completeness; SLO *recoverability*. |

> **Gap (O-4):** there is **no WS-specific metric** (attach attempts, attach failures, active connections, broadcast publish failures). On-call cannot today distinguish "WS is down" from "no one is watching". Add a `cooker_ws_connections` gauge + `cooker_ws_attach_failures_total` counter. (S; §5 OB-4.)

### 1.5 Error-budget policy

- **Burn-rate alerting** (multi-window): page when the 1h burn rate would exhaust the 30-day budget in < ~2 days (fast burn), ticket when the 6h window trends toward exhaustion (slow burn). The Google SRE 2%/5% multi-window thresholds are a good default.
- **Budget-exhaustion freeze:** when the hosted-SaaS monthly budget is spent, **freeze feature deploys**, only reliability fixes ship, until the window rolls. This is the lever that makes the SLO real rather than decorative.

---

## 2. Observability gap analysis

### 2.1 What exists today (confirmed in code, not just docs)

**Metrics — `backend/internal/observability/observability.go`, opt-in via `COOKER_METRICS_ENABLED=true`:**

| Metric | Type | Verified | On-call use |
|---|---|---|---|
| `cooker_http_requests_total{method,route,status}` | counter | L39 | Availability SLI, 5xx rate |
| `cooker_http_request_duration_seconds{method,route}` | histogram | L44 | Latency SLI |
| `cooker_db_connection_errors_total` | counter | L52 | **Resilience #1** — page |
| `cooker_redis_connection_errors_total` | counter | L57 | **Resilience #2** — page |
| `cooker_jwks_fetch_failures_total` | counter | L62 | **Resilience #3** — warn |
| `cooker_pipeline_runs_orphaned_total` | counter | L67 | **Resilience #4** — pods crashing |
| `cooker_pipeline_stage_duration_seconds{type,status}` | histogram | L75 | Build/deploy SLO |
| `cooker_audit_events_dropped_total` | counter | L81 | Audit-trail integrity |
| `cooker_run_heartbeat_errors_total` | counter | L86 | Run-coordinator health |
| `cooker_jobqueue_depth{status}` | gauge | L95 | Queue backlog |
| `cooker_jobqueue_attempts_total{kind}` | counter | L100 | Throughput |
| `cooker_jobqueue_run_duration_seconds{kind,outcome}` | histogram | L105 | Job latency |
| `cooker_notifier_sent_total{channel,outcome}` | counter | L111 | Notification delivery |

> **Confirmation:** the "four resilience metrics" referenced across docs are `cooker_{db,redis}_connection_errors_total`, `cooker_jwks_fetch_failures_total`, `cooker_pipeline_runs_orphaned_total`. They are present and wired. In reality the resilience-class surface is **richer than four** — `audit_events_dropped`, `run_heartbeat_errors`, `jobqueue_depth` and `notifier_sent` are all page/ticket-worthy and are *not* mentioned in the headline "four metrics" framing. The README and ROLLOUT under-sell the available signal.

**Traces:** OTel OTLP/gRPC exporter wired (`observability.go:16-23`, `otelgin` middleware), opt-in via `COOKER_OBSERVABILITY_TRACING_ENABLED=true`; stage spans link to the parent HTTP request (T18). Sound.

**Health endpoints (verified via RUNBOOK "Probe semantics" + ROLLOUT smoke checks):** `/health/live` (unconditional, → livenessProbe), `/health/ready` (1s DB ping + Redis ping + JWKS age, 503 + per-check breakdown, → readinessProbe), `/health` (back-compat alias). **No `startupProbe`** — flagged IN-H5 in the full audit; on slow-boot clusters liveness can SIGKILL before ready.

**Audit sink:** async, bounded queue (1024), drop-on-full (T16); `COOKER_AUDIT_DESTINATION` is a comma list (`stdout`/`file`/`db`); db sink gives the queryable `/admin/audit` viewer with a daily retention sweep. Drop counter + RUNBOOK alert exist. Sound design; the trade-off (drop > freeze) is operator-visible and documented.

**`/version`** (`/api/v1/version`) returns build SHA/commit/date from ldflags — scrape into a `build_info` gauge.

### 2.2 The gaps an on-call actually hits

| ID | Gap | Impact | Fix |
|---|---|---|---|
| **O-1** | **Dashboards referenced but absent.** RUNBOOK "Monitoring dashboards" says pre-built Grafana JSON lives in `deploy/observability/dashboards/` — **that directory does not exist.** Operators have metrics but no canonical view. | On-call builds dashboards from scratch mid-incident. | **Ship the dashboard JSON + recording rules** the RUNBOOK already promises. M. §5 OB-1. |
| **O-2** | **Alert rules are docs, not artifacts.** RUNBOOK ships *recommended* Alertmanager YAML inline; nothing in `deploy/` renders it. ROLLOUT Phase 4 tells the operator to *paste* them. | Every operator hand-copies; drift guaranteed. | **Ship `deploy/observability/alerts.yaml`** (PrometheusRule) + Helm toggle to install it. M. §5 OB-2. |
| **O-3** | **Build success metric conflates user vs platform failure** (§1.3). | Cannot write an honest build-success SLO. | Add `failure_class` label. S. §5 OB-3. |
| **O-4** | **No WS-stream metrics** (§1.4). | Can't alert on the most-visible surface. | `cooker_ws_connections` gauge + `cooker_ws_attach_failures_total`. S. §5 OB-4. |
| **O-5** | **No SLO/error-budget recording rules or burn-rate alerts.** | SLOs are aspirational, not measured. | Recording rules + multi-window burn alerts. M. §5 OB-5. |
| **O-6** | **No status page.** | Hosted-SaaS users have no self-serve incident visibility; every blip becomes a support ticket. | See §2.4. |

### 2.3 Dashboards & alerting to add (concrete)

**Dashboards (Grafana JSON to ship in `deploy/observability/dashboards/`):**
1. **Golden signals** — request rate / error-rate / p95-p99 latency by `route` (off `cooker_http_request_duration_seconds`); 5xx ratio gauge against the 99.5% line.
2. **Pipeline health** — stage duration p50/p95 by `type`, build/deploy success ratio, `cooker_jobqueue_depth` by status, attempts throughput.
3. **Resilience** — the six counters as single-stat "should be 0" panels + `build_info` title row from `/version`.
4. **Capacity** — `go_goroutines`, `container_memory_working_set_bytes`, Postgres `pg_stat_activity` count (ROLLOUT Phase 2 already watches these manually).

**Alerts to add beyond RUNBOOK's six** (the six existing: `CookerDB/Redis/JWKS/OrphanedRuns/AuditDropped/StageDurationHigh`):
- `CookerHighErrorRate` — 5xx ratio > 1% for 5m (SLO breach, page).
- `CookerSLOFastBurn` / `CookerSLOSlowBurn` — multi-window budget burn (page / ticket).
- `CookerReadinessFlapping` — `/health/ready` 503 from > 50% pods for 5m (ROLLOUT rollback trigger #2, not yet an alert).
- `CookerHeartbeatErrors` — `rate(cooker_run_heartbeat_errors_total[5m]) > 0` (exists as a metric, no alert).
- `CookerJobQueueBacklog` — `cooker_jobqueue_depth{status="pending"}` sustained > N (saturation).
- `CookerWSAttachFailures` — once O-4 lands.

### 2.4 Status page recommendation

- **Self-hosted:** none needed; operators use their own monitoring.
- **Hosted-SaaS:** **required before any contractual SLA.** Recommend a hosted status page (Instatus / Statuspage / BetterStack) driven by **synthetic probes** against `/health/ready` and a canary pipeline run, *not* by internal metrics (don't leak topology). Components to surface: **API**, **Build/Deploy execution**, **Live logs (WS)**, **Auth (OIDC)**. Wire the error-budget burn alerts to auto-open incidents. This is also the cheapest credibility win for the adoption story (`product-plan.md` §7 rung 1: a live demo + status page).

---

## 3. Reliability gaps for production

### 3.1 What's already handled (confirm from boot-resilience notes — verified)

From `backlog.md` "Production readiness summary" + RUNBOOK, all confirmed against the audit series:
- **Postgres down at boot** → jittered exponential backoff up to 5 min; pod self-reconnects (RUNBOOK "PostgreSQL is down").
- **IdP unreachable** → lazy OIDC discovery, retried every 30s; authenticated requests get `503 + Retry-After: 30`, existing sessions keep working; no crash-loop (RUNBOOK "OIDC issuer unreachable", ROLLOUT smoke #1).
- **Registry / secrets backend down** → scoped failure (reveal/push 5xx) without taking the API down; KeepSave circuit breaker (RUNBOOK "Secrets backend").
- **Hard kill** → orphan sweep marks stale `running` rows failed on next boot (RUNBOOK "Recovery after restart", ROLLOUT smoke #4).
- **Graceful shutdown** → SIGTERM drains HTTP ~30s + runs ~25s, `terminationGracePeriodSeconds: 60` (ROLLOUT smoke #5).
- **Redis blip (multi-replica)** → silences broadcasts but UI stays up; clients reconnect (MULTI_REPLICA "WS broadcast topology").

This is a **genuinely strong graceful-degradation story** and is the project's best reliability asset. The gaps below are about *data durability* and *recovery*, not boot resilience.

### 3.2 Backup / restore + DR

**Postgres is the only stateful component** (confirmed — RUNBOOK "Backup, retention, restore": pipelines, runs, environments, apps, hosts, users, schema_migrations, JSONB run history all in PG; schema re-creatable from empty via embedded migrations).

| Concern | Current state | Gap / recommendation |
|---|---|---|
| **Automated backup** | **None shipped.** Chart does **not** ship a backup operator. RUNBOOK lists *options* (Bitnami `backup.enabled`, Velero CSI snapshot, managed-PG PITR) but the operator must choose and wire it. | **Self-hosted:** document + provide a `pg_dump --format=custom` CronJob example in the chart (S). **Hosted-SaaS:** mandatory managed Postgres with WAL/PITR (Neon/Supabase/RDS/Cloud SQL). |
| **RPO** | Undefined. | **Self-hosted target: ≤ 24h** (nightly `pg_dump`). **Hosted-SaaS target: ≤ 5 min** (WAL/PITR). State the RPO explicitly per tier — today it's silent. |
| **RTO** | Restore drill recipe exists (RUNBOOK, quarterly), `< 1h` is the implied bound ("if restore takes > 1h your backup format is wrong"). | **Self-hosted: ≤ 1h. Hosted-SaaS: ≤ 30 min.** The restore drill is documented but **there is no evidence it's been rehearsed** — launch-readiness §6 lists it as an *unchecked box*. **Rehearse before any launch.** |
| **Secret-key loss = data loss** | Documented hard edge: `COOKER_SECRET_KEY` loss means all AES-GCM-sealed secrets are unrecoverable (RUNBOOK restore step 3; dual-key rotation is roadmap R1). | Back up `COOKER_SECRET_KEY` **separately** from the DB (different blast radius). Make this a launch-checklist line item. |
| **KeepSave-as-source-of-truth** | If `COOKER_SECRETS_BACKEND=keepsave`, there is **no DB fallback** — KeepSave's own backup is the only copy (RUNBOOK). | DR plan must include the external secrets backend's backup story, not just Postgres. |

### 3.3 Multi-region considerations

Out of scope for v1 (correctly). For the record so it's not a surprise later:
- **Stateless tier** (the Go binary) scales horizontally already (Redis backends, MULTI_REPLICA.md). Multi-region of the *app* tier is feasible.
- **The hard part is Postgres.** Active-active is not designed for; a warm-standby region with async-replicated PG (RPO = replication lag) is the realistic v2 shape. The `Codec` single-key model (R1) and single-issuer OIDC (R2) are *not* blockers for multi-region but become operationally awkward across regions.
- **Recommendation:** do **not** promise multi-region in v1 SLA. Single-region 99.5% is the honest hosted ceiling until PG replication + a tested region-failover runbook exist (which ROLLOUT explicitly says it "does not cover").

### 3.4 Residual reliability risks worth tracking

The 2026-06 full audit ([`audits/2026-06-full-audit.md`](../audits/2026-06-full-audit.md)) found six CRITICALs — all marked **fixed-in-that-PR** (CR-1 hub deadlock, CR-2 Vault race, CR-3 build goroutine leak, CR-4/CR-5 raw-manifest probe/secret-key, CR-6 WS authz). **Two reliability-relevant items to confirm landed on `main` before SLA sign-off:**
- **CR-1** (`server/wshub_backend.go:80`) — memory hub `Publish` blocking-send could *deadlock stage execution* on a full buffer. Directly threatens the build-success SLO. Confirm the fix is on `main`.
- **CR-3** (`builder/kaniko.go:144`, `buildah.go:120`) — per-build goroutine + kube-conn leak → slow OOM, which is an availability risk under sustained build load. Confirm fixed.
- Also: **raw `deploy/kubernetes/` manifests lag the Helm chart** (CR-4/CR-5, IN-H1..H6 — missing `startupProbe`, `COOKER_SECRET_KEY`, `COOKER_ENV`, PDB, `:latest` pins). **Operators on the raw-manifest path do not get the chart's safety guards.** For a reliable launch, **recommend the Helm chart as the only supported install path** and label raw manifests "reference only".

---

## 4. Incident readiness

| Area | Current state | Gap | Recommendation |
|---|---|---|---|
| **Runbook** | **Strong.** RUNBOOK covers 11 symptom→cause→mitigation scenarios + restore drill + secrets-backend failure matrix. Among the best-developed launch assets. | **Coverage holes:** (a) audit-logging *middleware* section says "not yet implemented (P1.2)" — stale if P1.2 landed; reconcile. (b) No runbook for **WS-stream outage**, **job-queue backlog**, or **cert/TLS expiry**. (c) No "SLO budget exhausted → freeze" procedure. | Add the three missing scenarios; reconcile the P1.2 note; add the budget-freeze procedure (ties to §1.5). M. |
| **On-call rotation** | **Not instantiated.** RUNBOOK escalation ladder (L1 platform on-call / L2 maintainer / L3 leadership) is a *template* with placeholder owners. launch-readiness pre-PROD explicitly flags "update with real names + tier numbers" as unchecked. | No actual rotation, no PagerDuty/Opsgenie config, no contact list. | **Self-hosted:** N/A (operator's own). **Hosted-SaaS:** stand up a real rotation + paging tool before launch. Solo-maintainer reality (`product-plan.md`): a 1-person "rotation" is acceptable for best-effort but **incompatible with a 99.5% contractual SLA** — the SLA implicitly requires ≥ 2 responders. |
| **Escalation** | Ladder defined; data-loss incidents (sustained audit drops, PG restore, key loss) correctly route security/compliance in parallel. | Owners are placeholders. | Fill in; wire alert routing to the pager. |
| **Postmortem** | launch-readiness pre-PROD says "internal post-mortem template ready" — **no template exists in-repo** (no `docs/.../postmortem*`). | No blameless-postmortem template, no incident log, no action-item tracking. | Add `docs/launch/postmortem-template.md` (blameless: timeline, impact, root cause, contributing factors, action items w/ owners). S. |
| **Game days** | None. | No induced-failure rehearsal beyond ROLLOUT's 7 smoke checks (which are deploy-time, not incident-response drills). | Pre-launch: run one game day exercising PG failover + restore + a deliberate orphan sweep. M. |

---

## 5. Launch SRE checklist + effort & priority

Effort: **S** ≤ ½ day · **M** ≤ 2 days · **L** ≤ 1 week · **XL** > 1 week. Priority: **P0** SLA-blocking for hosted-SaaS / strongly advised for self-hosted · **P1** before first paying customer · **P2** post-launch hardening.

| # | Item | Tier it blocks | Effort | Priority |
|---|---|---|---|---|
| OB-1 | Ship the Grafana dashboards RUNBOOK already references (`deploy/observability/dashboards/`) — close the **dangling-reference gap O-1** | both | M | **P0** |
| OB-2 | Ship `PrometheusRule` alert bundle + Helm toggle (codify RUNBOOK's six + §2.3 additions) | both | M | **P0** |
| OB-5 | SLO recording rules + multi-window burn-rate alerts | hosted | M | **P0** |
| BK-1 | **Rehearse the restore drill** (it's documented, never proven) + state RPO/RTO per tier | both | S | **P0** |
| BK-2 | Provide a `pg_dump`/PITR backup CronJob example in the chart (no operator today) | self-hosted | S | **P0** |
| KEY-1 | Document + checklist: back up `COOKER_SECRET_KEY` separately from the DB | both | S | **P0** |
| IR-1 | Reconcile RUNBOOK P1.2 audit note; add WS-outage / queue-backlog / TLS-expiry / budget-freeze scenarios | both | M | **P1** |
| IR-2 | Add blameless postmortem template (`docs/launch/`) + incident-log convention | hosted | S | **P1** |
| OB-3 | Add `failure_class` (user/platform) label to stage metric → honest build-success SLO | hosted | S | **P1** |
| OB-4 | Add WS metrics (`cooker_ws_connections`, `cooker_ws_attach_failures_total`) | hosted | S | **P1** |
| OC-1 | Stand up on-call rotation + paging tool + fill escalation owners | hosted | M | **P1** |
| ST-1 | Status page (synthetic-probe driven) with 4 components | hosted | M | **P1** |
| INF-1 | Add `startupProbe` (IN-H5); declare Helm the only supported install path, label raw manifests "reference only" | both | S | **P1** |
| CR-VERIFY | Confirm CR-1 (hub deadlock) + CR-3 (build leak) fixes are on `main` before SLA sign-off | both | S | **P1** |
| GD-1 | One pre-launch game day (PG failover + restore + orphan sweep) | hosted | M | **P2** |
| OB-6 | Resolve metrics opt-in: `COOKER_METRICS_ENABLED` defaults **false** — ensure prod values turn it on (no metrics = no SLO) | both | S | **P2** |
| DR-1 | Design warm-standby multi-region (PG async replica + region-failover runbook) — do **not** promise in v1 | hosted v2 | XL | **P2** |

### Priority ranking (do-this-order)

1. **OB-1, OB-2, BK-1, BK-2, KEY-1, OB-5** — the P0 SLA-blockers. Without dashboards+alerts+a proven restore, "production-ready" is aspirational. **~1 week.**
2. **IR-1, IR-2, OB-3, OB-4, OC-1, ST-1, INF-1, CR-VERIFY** — required before a *paying* hosted customer. **~1 week.**
3. **GD-1, OB-6, DR-1** — post-launch hardening / v2.

---

## Appendix — claims cross-check

| Claim in this doc | Source verified |
|---|---|
| Four resilience metrics present + named | `backend/internal/observability/observability.go:52-70` |
| Actually 6+ resilience-class series (audit/heartbeat/jobqueue/notifier) | same file, L81-114 |
| `/metrics` opt-in via `COOKER_METRICS_ENABLED` (default false) | README L444; CHANGELOG L373 |
| `/health/live` unconditional; `/health/ready` = DB+Redis+JWKS, 503 breakdown | RUNBOOK "Probe semantics" |
| **Dashboards dir does not exist** | `ls deploy/observability/` → No such file or directory; RUNBOOK L345 references it |
| Backup operator not shipped; restore drill documented not proven | RUNBOOK "Backup, retention, restore"; launch-readiness §6 (unchecked) |
| Boot resilience (PG backoff / lazy OIDC / SIGTERM drain / orphan sweep) | backlog "Production readiness summary"; RUNBOOK; ROLLOUT smoke 1–6 |
| On-call ladder + postmortem are placeholders/absent | RUNBOOK "On-call escalation"; launch-readiness pre-PROD checklist |
| CR-1/CR-3 reliability CRITICALs (marked fixed-in-PR) | `audits/2026-06-full-audit.md` §TL;DR + Remediation |
| Single-replica is "ship it" but not HA | `backlog.md` deployment-shape matrix row 1 |
