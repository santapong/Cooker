# Launch readiness — Cooker pre-UAT / pre-PROD checklist

**Companion to:** [`dag-performance.md`](dag-performance.md), [`spof-and-database.md`](spof-and-database.md), [`crash-and-service-quality.md`](crash-and-service-quality.md), [`vulnerabilities-and-chains.md`](vulnerabilities-and-chains.md), [`chain-recheck.md`](chain-recheck.md), [`remediation-plan.md`](remediation-plan.md).

This is the operator-facing wrap-up of the audit + remediation series. It answers two questions:

1. **Is Cooker safe to test in UAT and launch to production?** — yes, with caveats below.
2. **What's deferred to a post-launch follow-up, and why isn't it launch-blocking?** — itemised.

The rest of this doc is a checklist + a roadmap. Skim the checklist before pushing to UAT; skim the roadmap before promising stakeholders that "everything is done."

---

## TL;DR

| | Status |
|---|---|
| Phase 0 (security hot-fixes T1, T2, T3, T5) | ✅ Landed |
| Phase 1 (stability T6–T9) | ✅ Landed |
| Phase 2 (reliability T10–T14) | ✅ Landed |
| Phase 3 (production hardening T15–T19) | ✅ Landed |
| Phase 4 (polish T20–T24) | ✅ Landed |
| Launch-prep hardening (W1–W5) | ✅ Landed |
| Chain coverage | 19 / 54 closed; 0 launch-blockers in the remaining 28 |
| Tests | `go test -race ./...` green; helm CI green; CI gates all pass |
| Audit docs | All five primary docs current; cross-references updated |

**Recommendation: ship to UAT.**

---

## Pre-UAT checklist

Walk this in order before flipping `COOKER_ENV=uat` on:

### 1. Configuration

- [ ] `COOKER_ENV=uat` (or `production`).
- [ ] `DATABASE_URL` set to a real Postgres URL (not the dev default `cooker:cooker@localhost`). `Validate()` will reject the dev default in production mode (T19).
- [ ] `COOKER_SECRET_KEY` set to a base64-encoded 32-byte key. Required when secrets backend is anything other than `keepsave`. `Validate()` enforces (T19).
- [ ] `COOKER_OIDC_*` configured against the real IdP if `cookerEnv=production`. `auth-methods` will refuse to advertise OIDC otherwise.
- [ ] `COOKER_ALLOWED_ORIGINS` set to the exact ingress hostnames. Wildcard `*` is rejected in production (T19).
- [ ] If using `keepsave`: `COOKER_SECRETS_KEEPSAVE_URL` is `https://`. `http://` is rejected in production (T19).
- [ ] If running `replicaCount > 1`: set `COOKER_WS_HUB_BACKEND=redis`, `COOKER_WS_TICKET_BACKEND=redis`, `COOKER_RATE_LIMIT_BACKEND=redis`. The in-memory backends are per-process; tickets and rate limits will misbehave across replicas otherwise. The chart already defaults these to `redis` in `values.yaml`.

### 2. Helm-chart settings

- [ ] `replicaCount` matches your cluster's HPA / drain tolerance.
- [ ] `hpa.enabled: true` if you want autoscaling (T17). Defaults off.
- [ ] `pdb.enabled: true` whenever `replicaCount > 1` (T17). Defaults off.
- [ ] Probes (T17): `liveness.timeoutSeconds`, `readiness.timeoutSeconds`, `failureThreshold` — defaults are deliberately generous; tune once you've measured boot time.
- [ ] `secretKey.existingSecret` references a pre-created Secret (chart doesn't carry the inline value through `optional: false` after T24).
- [ ] If `builder.kind: kaniko` or `buildah`: `builder.<kind>.contextPVC` is set. The Helm chart's `required` guard will fail-fast if not.
- [ ] Resource `limits` set on Kaniko / Buildah Jobs (T8 sets sensible defaults: 2 CPU, 4Gi memory).

### 3. RBAC + network

- [ ] If using the raw `deploy/kubernetes/` manifests: T5's split — namespaced Role for the cooker namespace + a `cooker-builders` Role for whichever namespace your builder runs in. The cluster-wide `ClusterRole` is gone.
- [ ] `NetworkPolicy` enabled in the chart and your CNI enforces it. Default chart values turn this on.
- [ ] Ingress with TLS terminator in front of cooker. Cooker itself listens on plain HTTP (T19's note); `chartEnv=production + ingress.enabled=true` without `ingress.tls[]` is now refused at `helm template` time.

### 4. Smoke tests in UAT

After deploy:

- [ ] `GET /health/live` returns 200.
- [ ] `GET /health/ready` returns 200; the body's `checks` block shows `db:ok`, `redis:ok` (if Redis configured), `jwks_age_s` reasonable.
- [ ] `GET /version` returns the build SHA you intended (T22). Confirm via `kubectl exec`.
- [ ] An OIDC sign-in works end-to-end and the session token is accepted on a subsequent API call.
- [ ] Create a pipeline, trigger a run, watch the WebSocket log stream attach. Confirm `StageRun.Logs` is populated after the run finishes (T13 wired this end-to-end).
- [ ] Run the same pipeline twice with the same `Idempotency-Key` header — second response carries `Idempotency-Replayed: true` and the same run ID (T12).
- [ ] PATCH the same pipeline twice with stale `version` field — second returns 409 (T11).

### 5. Observability

- [ ] Prometheus scrapes `/metrics` and the new histograms (`cooker_pipeline_stage_duration_seconds`, T18) and counters (`cooker_audit_events_dropped_total`, `cooker_run_heartbeat_errors_total`) appear.
- [ ] Tracing collector receives spans from cooker if `COOKER_OBSERVABILITY_TRACING_ENABLED=true`. Confirm a stage's span links back to the parent HTTP request (T18).
- [ ] Audit log file (or stdout sink) is rotated on a sane cadence — async writer drops on overflow rather than blocking, but a sustained drop rate means the SIEM is missing events. Wire the `CookerAuditEventsDropped` alert from `RUNBOOK.md`.

### 6. Backup + restore

- [ ] Postgres has WAL archiving / point-in-time-restore enabled. Cooker's chart does NOT ship a backup operator (T23 in `RUNBOOK.md`).
- [ ] You've run a restore drill in a separate cluster. The drill recipe is in `RUNBOOK.md` § Backup, retention, restore.
- [ ] Retention policy in place — `pipeline_runs` grows without bound otherwise. Suggested: `DELETE FROM pipeline_runs WHERE finished_at < NOW() - INTERVAL '90 days'`.

---

## Pre-PROD checklist (after UAT proves out)

In addition to all of the above:

- [ ] `COOKER_ENV=production` (gates strict CORS defaults + boot-time `Validate()` errors).
- [ ] Multi-replica with the Redis backends actually wired in. Sticky sessions optional once Redis is the source of truth for tickets / rate limit.
- [ ] OIDC step-up MFA configured for admin destructive routes (`oidc.mfaAcrValues`).
- [ ] `cooker-weekly` workflow turned on by setting `COOKER_WEEKLY_ENABLED=true` (workflow ships disabled).
- [ ] `ANTHROPIC_API_KEY` provisioned for the weekly workflow if you're using the `cooker-weekly` skill via Actions.
- [ ] `RUNBOOK.md` paged on-call escalation ladder (T23) updated with real names + tier numbers.
- [ ] Internal post-mortem template ready in case the first prod incident is a known chain — half of `chain-recheck.md`'s open items have an ID for that.

---

## Post-launch roadmap (NOT launch-blockers)

These are real findings from the audit series that a maintainer should plan for, in rough priority order. None of them blocks UAT or PROD launch — they're sustained-load and operational-flexibility items.

| # | Item | Why deferred | Audit reference |
|---|---|---|---|
| R1 | Dual-key `COOKER_SECRET_KEY` rotation | Needs a `Codec` design that accepts two keys at once during a rolling key change. Single-key rotation works today with a coordinated restart. | B.4.3, B.7.7 |
| R2 | OIDC dual-issuer / dual-client-id rotation | Same shape as R1; the OIDC verifier is currently single-issuer. Coordinated restart works for now. | B.4.1, B.7.6 |
| R3 | JWKS forced age-based refresh | go-oidc's cache is opaque; needs an explicit refresh tick or a CVE-aware cache wrapper. Today the cache is good for typical IdP rotation cadences. | B.2.4, B.4.2, B.3.6 |
| R4 | Run-pipeline snapshot | Pin pipeline JSON onto the run row at `Execute` entry so editing the pipeline mid-run can't change the executor's view. Today the handler creates a new struct on Update so the chain is theoretical, but defensive. | B.8.2 |
| R5 | WS log replay buffer | Keep last N stage-log lines in Redis so a WS reconnect to a different replica doesn't lose history. Today `StageRun.Logs` persists on stage-finish so historical lines are available via `GET /runs/:id`. | B.8.3 |
| R6 | Redis-backed `idempotency.Store` | Per-replica in-memory cache works for sticky sessions; multi-replica without sticky sessions can dedup-miss. Interface is already in place. | "Newly introduced" #1 |
| R7 | K8s API circuit breaker | Wraps Kaniko / Buildah / clientgo deployer calls. Today's per-stage timeout (T10) bounds a stuck call to one stage's worth of damage. | B.3.1, B.3.3 |
| R8 | Bounded global concurrency cap | Per-pipeline cap exists (T10 + W3 = `COOKER_DAG_MAX_PARALLEL`); a global cap across all in-flight runs would prevent a fleet of pipelines from saturating the cluster's K8s API. | B.6.8 follow-up |
| R9 | `alpine:3.19` pinned to `@sha256:` digest | Tag-pinning is good supply-chain hygiene; the alpine 3.19 line is itself reasonably stable. Schedule alongside a digest-pinning workflow that bumps on each apk advisory. | T24 follow-up |
| R10 | Configurable Stage `RetryBackoff` per type | T10's `Stage.Config.Retries` is honoured but every retry uses the package default backoff. Per-type tuning (registry pushes want longer; tests want fail-fast) is the natural extension. | T10 follow-up |

The `cooker-weekly` skill (`.claude/skills/cooker-weekly/`) is calibrated to land **one** of these per week as the open chain count drops.

---

## Known issues that *are* visible in UAT but won't break it

The remediation series has good coverage, but these are the things you'll see in UAT that are *known* and *non-blocking*:

1. **A pipeline run that exceeds 30 minutes will fail with `context deadline exceeded`.** Set `COOKER_RUN_DEADLINE=2h` (or wherever your real ceiling is) — W2.
2. **DAG levels with > 16 stages will queue.** Set `COOKER_DAG_MAX_PARALLEL` if your cluster has the headroom — W3.
3. **Idempotency cache resets on replica restart.** Webhook retries that span a restart will spawn duplicate runs. Use the Redis backend (post-launch follow-up R6) for fleets that can't tolerate this.
4. **Audit-log dropped events on disk-full.** Drop counter exposed; alert wired in `RUNBOOK.md`. Drop is preferable to API freeze; the audit-trail loss is the trade-off.
5. **Long-running gitops push has no per-call timeout.** The new `runDeadline` (T-deadline) bounds the whole stage; per-call is finer-grained but not yet wired.

---

## How to use this doc going forward

- **Before each UAT push:** re-walk the Pre-UAT checklist. The checks aren't expensive and they catch config drift.
- **Before each PROD push:** re-walk Pre-PROD additions. The big ones are MFA + retention policy + restore drill.
- **When chain-recheck shows a new finding closed by remediation:** delete the corresponding row from the post-launch roadmap.
- **When a UAT or PROD incident lines up with one of the post-launch roadmap items:** promote that item out of the roadmap into the next sprint and link the incident.
