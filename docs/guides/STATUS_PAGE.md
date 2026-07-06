# Status Page — Recommendation & Setup Guide

**Status:** recommendation. **Grounding:** [`docs/launch/00-sre-sla-readiness.md`](../launch/00-sre-sla-readiness.md) §2.4 (status-page recommendation) and §1 (SLO targets), [`docs/legal/sla.md`](../legal/sla.md).

> This guide is a **recommendation plus a ready-to-copy config snippet**. It does **not**
> add any live workflow under `.github/workflows/` — wiring it up is an explicit operator
> step. Treat the Upptime snippet below as a starting template.

---

## TL;DR

- **Self-hosted:** a public status page is **optional**. Operators use their own monitoring.
- **Hosted-SaaS:** a public status page is **required before any contractual SLA**
  ([`docs/legal/sla.md`](../legal/sla.md) §2.7) — it is also the cheapest credibility win
  for adoption.
- **Drive it from synthetic probes against `/health/ready`** (and a canary pipeline run),
  **not** from internal metrics — don't leak topology.
- Surface four components: **API**, **Build/Deploy execution**, **Live logs (WS)**,
  **Auth (OIDC)**.

---

## 1. Why probe `/health/ready` (not `/metrics` or `/health/live`)

Cooker exposes three health surfaces (see `docs/guides/RUNBOOK.md` "Probe semantics"):

| Endpoint | Meaning | Use for status page? |
|---|---|---|
| `/health/live` | Process is up (unconditional) | No — too shallow; says nothing about dependencies. |
| `/health/ready` | Does a ~1s **DB ping + Redis ping + JWKS-age check**, returns 503 + a per-check breakdown when unhealthy | **Yes** — this is the real "can it serve traffic" signal. |
| `/metrics` | Prometheus metrics (opt-in, `COOKER_METRICS_ENABLED`) | No — internal; exposes topology, and is auth/scrape-scoped. |

A status checker should treat **HTTP 200 from `/health/ready` = operational** and **503 /
timeout / connection failure = degraded or down**. The 503 body breaks down which
dependency failed (DB / Redis / JWKS), which is useful for incident notes but should **not**
be published verbatim (it reveals topology).

> **Public endpoint guidance.** Do **not** expose `/health/ready` publicly just for the
> status page if it isn't already reachable. Options, in order of preference:
> 1. Probe from inside your network and push results to a hosted status page (most hosted
>    providers support a push/heartbeat API).
> 2. Expose a **dedicated, unauthenticated, rate-limited public health route** at the edge
>    (ingress/load-balancer) that proxies `/health/ready` and **strips the dependency-
>    breakdown body**, returning only the status code. Never expose `/metrics` publicly.
> 3. Use a **canary pipeline run** as a deeper synthetic check for the Build/Deploy
>    component (a real end-to-end signal, not just "the API answers").

## 2. Components to surface

Map probes to the same four components the SLA reasons about:

| Component | Synthetic probe |
|---|---|
| **API** | HTTP GET `/health/ready` → expect 200 |
| **Build/Deploy execution** | Trigger a tiny **canary pipeline** on a schedule; assert it reaches `success`. Falls back to `/health/ready` if a canary isn't wired yet. |
| **Live logs (WebSocket)** | Open a WS using the `POST /api/v1/ws-tickets` → `?ticket=` flow and assert first frame. (No public metric yet — `docs/launch/00-sre-sla-readiness.md` O-4.) |
| **Auth (OIDC)** | Probe the OIDC discovery/JWKS reachability indirectly: a 503 from `/health/ready` with a JWKS-age failure indicates Auth degradation. |

## 3. Recommended approaches

| Option | Cost | Best for | Notes |
|---|---|---|---|
| **Upptime** (GitHub-Actions-driven, GitHub Pages) | Free | Self-hosted / OSS / early launch | Probes from GitHub runners, history in git, page on GH Pages. Config snippet below. **No private network access** — needs a publicly reachable probe target (see §1 guidance). |
| **Hosted status page** (Instatus / BetterStack / Atlassian Statuspage) | Paid | Hosted-SaaS / contractual SLA | Synthetic monitors + incident workflow + subscriber notifications; can probe from multiple regions and via push API from inside your network. **Recommended for the hosted contractual SLA.** |

**Recommendation:** start with **Upptime** for the self-hosted/OSS project status page
(free, version-controlled, zero infra). When the **hosted-SaaS** tier launches, move to a
**hosted provider** (BetterStack or Statuspage) so you get multi-region probes, an incident
workflow, and subscriber notifications, and **wire the error-budget burn-rate alerts
(`docs/launch/00-sre-sla-readiness.md` OB-5) to auto-open incidents.**

## 4. Ready-to-copy Upptime config snippet

Upptime is configured by a single `.upptimerc.yml` at the repo root of a **separate
status repo** (keep it out of the product repo so probe history doesn't churn this one).
Create a repo (e.g. `cooker-status`), drop this file in, and follow Upptime's setup to
enable the bundled GitHub Actions and GitHub Pages.

> Replace `https://status-probe.cooker.example.com/...` with your **edge health route** from
> §1 (status-code-only, body stripped). Do **not** point Upptime at `/metrics` or at an
> internal hostname.

```yaml
# .upptimerc.yml  — place in a dedicated status repo (e.g. cooker-status)
owner: your-org              # GitHub org/user that owns the status repo
repo: cooker-status         # the status repo name

sites:
  - name: API
    url: https://status-probe.cooker.example.com/health/ready
    # expect HTTP 200; Upptime treats >=400 / timeout as down
    maxResponseTime: 1500     # ms — aligns with the /health/ready p95 SLO
    __dangerous__insecure: false

  - name: Auth (OIDC)
    # A 503 from /health/ready with a JWKS-age failure surfaces as Auth degradation.
    url: https://status-probe.cooker.example.com/health/ready
    maxResponseTime: 2000

  - name: Build/Deploy execution
    # Prefer a canary-pipeline webhook/heartbeat URL once wired; fall back to readiness.
    url: https://status-probe.cooker.example.com/health/ready
    maxResponseTime: 2000

  - name: Live logs (WebSocket)
    # Upptime does basic HTTP checks; for a true WS check use an external synthetic monitor.
    # As a proxy, point at the API health route until a WS synthetic monitor exists.
    url: https://status-probe.cooker.example.com/health/ready
    maxResponseTime: 2000

status-website:
  cname: status.cooker.example.com   # optional custom domain for the GitHub Pages site
  name: Cooker Status
  introTitle: "Cooker Status"
  introMessage: "Live and historical availability for the Cooker service."

# Probe cadence and alerting are configured via Upptime's bundled workflows in the
# status repo (uptime, response-time, graphs, site, summary). Enable them there.
# Do NOT copy those workflows into the Cooker product repo.
```

## 5. What NOT to do

- **Don't** publish the `/health/ready` 503 body (the DB/Redis/JWKS breakdown) — it leaks
  topology. Publish status codes only.
- **Don't** expose `/metrics` publicly.
- **Don't** drive the status page from internal Prometheus metrics directly; use synthetic
  probes so the page reflects the *user-visible* experience and can't itself go down with
  the metrics stack.
- **Don't** add the Upptime/monitor workflows to `.github/workflows/` in **this** repo —
  they belong in a dedicated status repo (and this guide intentionally ships none).

---

**Related:** [`docs/launch/00-sre-sla-readiness.md`](../launch/00-sre-sla-readiness.md) §2.4 · [`docs/legal/sla.md`](../legal/sla.md) · `docs/guides/RUNBOOK.md` (probe semantics)
