> ⚠️ **TEMPLATE — requires review by qualified legal counsel before use.**
> This is a non-binding draft, not legal advice. A contractual SLA creates real financial
> obligations (service credits) and must be reviewed by counsel and confirmed against your
> actual operational capacity before being offered. Fill in every `[BRACKET]`.

# Cooker — Service Level Agreement (Template)

**Last updated:** [DATE]

This document describes Cooker's service-level posture for two deployment models. The
targets below are grounded in the SLO analysis in
[`docs/launch/00-sre-sla-readiness.md`](../launch/00-sre-sla-readiness.md) §1.

> **Read this first.** Today, **only a best-effort, non-contractual tier is offered**
> ([§1](#1-self-hosted--best-effort-non-contractual)). The hosted contractual SLA
> ([§2](#2-hosted-saas--contractual-sla-placeholder)) is a **placeholder** that **cannot be
> offered** until the SLA-blocking reliability and observability gaps in
> `docs/launch/00-sre-sla-readiness.md` §5 (dashboards, alerts, a *proven* restore drill,
> HA, on-call rotation, status page) are closed.

---

## 1. Self-hosted — best-effort (non-contractual)

**Applies to:** operators who run Cooker on their own infrastructure (Helm chart / binary).

In the self-hosted model, **availability depends entirely on how you operate the
software** — your cluster, your Postgres, your replica count, your monitoring. Cooker the
project therefore provides **targets and tooling, not a contractual promise**. There are
**no service credits** and **no uptime guarantee** for self-hosted use.

### 1.1 Best-effort targets

| Aspect | Best-effort target | Conditions |
|---|---|---|
| Availability | **~99.0%** (target, **no commitment**) | Only realistic with **≥ 2 replicas + a PodDisruptionBudget**. Single-replica is documented as "ship it" but is **not HA** — a pod restart blanks the UI. |
| Build success rate | **~95%** (platform-side) | Excludes user-caused build failures (your Dockerfile is broken ≠ our fault). |
| Deploy success rate | **~97%** (platform-side) | Excludes your app crash-looping after a successful deploy. |
| Read API latency | p95 < 300 ms, p99 < 800 ms | On adequately-resourced infra. |
| Mutating API latency | p95 < 500 ms, p99 < 1.5 s | |

### 1.2 What you must do to reach the targets
- Run **≥ 2 replicas** with a PDB; use the Redis-backed rate-limit / WS-ticket / WS-hub
  backends for multi-replica (see `docs/guides/MULTI_REPLICA.md`).
- Run a backed-up, ideally managed, **Postgres** with a tested restore (target self-hosted
  RPO ≤ 24h, RTO ≤ 1h — see `docs/launch/00-sre-sla-readiness.md` §3.2).
- Back up `COOKER_SECRET_KEY` **separately** from the database (its loss = permanent secret
  loss).
- Enable metrics (`COOKER_METRICS_ENABLED=true`) and wire alerting on the resilience
  counters.

### 1.3 What's explicitly excluded
No commitment, no credits. Cooker provides documentation
(`SECURITY.md`, the install / runbook / rollout guides), health endpoints
(`/health/live`, `/health/ready`), and `/metrics`, but does not operate, monitor, or
guarantee your instance.

---

## 2. Hosted-SaaS — contractual SLA (PLACEHOLDER)

> **PLACEHOLDER — not yet offered.** The hosted Cooker service does not exist today. The
> values below are the **intended first contractual commitment**, included so the
> commercial terms can be drafted and reviewed in advance. **Do not publish or sign this as
> a live SLA** until the §5 SLA-blockers in `docs/launch/00-sre-sla-readiness.md` are
> closed and the targets are validated against real operations.

### 2.1 Availability commitment

| Tier | Availability commitment | Window |
|---|---|---|
| Hosted-SaaS v1 | **99.5%** | Monthly (calendar month) |
| Hosted-SaaS v2 (aspirational — do **not** promise at launch) | 99.9% | Monthly |

**"Monthly Uptime Percentage"** = `(Total minutes in the month − Downtime minutes) / Total
minutes in the month`, where **Downtime** is sustained 5xx error rate on the production API
excluding `/health/*` probe traffic and excluding 4xx client errors, per the SLI definition
in `docs/launch/00-sre-sla-readiness.md` §1.1:
`1 − (rate(5xx, route!~"/health.*") / rate(all, route!~"/health.*"))`.

### 2.2 Service credits (placeholder schedule)

| Monthly Uptime Percentage | Service credit (% of monthly fee) |
|---|---|
| < 99.5% and ≥ 99.0% | **[10]%** |
| < 99.0% and ≥ 95.0% | **[25]%** |
| < 95.0% | **[50]%** |

Credits are the **sole and exclusive remedy** for availability failures, must be requested
within **[30]** days of the affected month, and are capped at **[the monthly fee]**.

### 2.3 Latency & success-rate objectives (objectives, not credit-bearing unless stated)
- Read API p95 < 300 ms / p99 < 800 ms; mutating API p95 < 500 ms / p99 < 1.5 s.
- Platform build success ≥ 95%, deploy success ≥ 97% (split user vs platform failures via
  the `failure_class` metric label — `docs/launch/00-sre-sla-readiness.md` §1.3 / OB-3).
- WebSocket log-stream attach success ≥ 99% once WS metrics land (O-4).

### 2.4 Exclusions
The commitment does **not** cover downtime caused by: (a) factors outside our control or
the control plane (your cluster, your registry, your IdP, your network); (b) your code,
configuration, or AUP violations; (c) scheduled maintenance announced at least **[48]**
hours in advance via the [status page](../guides/STATUS_PAGE.md); (d) force majeure;
(e) suspension for non-payment or AUP breach; (f) beta/preview features.

### 2.5 Backup & recovery objectives (hosted)
- **RPO ≤ 5 min** (managed Postgres with WAL/PITR).
- **RTO ≤ 30 min** (with a *rehearsed* restore drill — currently unproven; see
  `docs/launch/00-sre-sla-readiness.md` §3.2 BK-1).

### 2.6 Incident communication
Status, incident history, and maintenance windows are published on the
**[status page](../guides/STATUS_PAGE.md)**. Severity-1 incidents are posted within
**[15]** minutes of detection. Breach-notification commitments are governed by the DPA and
applicable law (GDPR 72-hour authority window).

### 2.7 Preconditions before this SLA can be offered
From `docs/launch/00-sre-sla-readiness.md` §5 (all **P0/P1** SLA-blockers):
1. Shipped Grafana dashboards + `PrometheusRule` alert bundle (OB-1, OB-2).
2. SLO recording rules + multi-window burn-rate alerts (OB-5).
3. A **rehearsed** Postgres restore drill with stated RPO/RTO (BK-1).
4. HA defaults: ≥ 2 replicas, Redis backends, managed Postgres with PITR.
5. A real **on-call rotation** (a 99.5% SLA implicitly requires ≥ 2 responders — a solo
   "rotation" is incompatible with a contractual SLA).
6. A live, synthetic-probe-driven **status page** (see `docs/guides/STATUS_PAGE.md`).

---

**Related:** [Terms of Service](terms-of-service.md) · [Status page plan](../guides/STATUS_PAGE.md) · [SRE / SLA readiness](../launch/00-sre-sla-readiness.md)
