# Rollout playbook — UAT to production

How to take a Cooker change from `main` to production safely. Use this as the single source of truth for cutovers; cross-references `RUNBOOK.md`, `MULTI_REPLICA.md`, `SECURITY.md`, and `backlog.md` rather than duplicating them.

> Backlog reference: P0.8.

## TL;DR

1. **Phase 0** — All CI green on `main`. Squash-merge.
2. **Phase 1** — Deploy to UAT, run the 7 smoke checks (~30 min hands-on).
3. **Phase 2** — Soak in UAT for ≥ 1 week, watch the 4 resilience metrics.
4. **Phase 3** — Ship code follow-ups `P0.1` and `P0.2` if the production deployment expects > 100 QPS or HA WS broadcasts.
5. **Phase 4** — Production cutover via `helm upgrade`, watch for 15 min.
6. **Phase 5** — Rollback plan ready (`helm rollback`).

If you skip a phase, you have signed up for the failure mode that phase exists to catch. The whole point of UAT is to find regressions before customers do.

---

## Phase 0 — Pre-merge gate

Required CI checks, all green on the PR:

- `backend` — Go build + vet + test (`-race`, against a Postgres service).
- `frontend` — `npm ci` + `lint` + `build` + `test`.
- `helm` — `helm lint` + `helm template` + `kubeconform`.
- `docker` — `docker build -f deploy/docker/Dockerfile .`.

Optional / non-blocking:

- `conformance` — runs on `workflow_dispatch` + weekly `schedule` (per `backlog.md` `P0.6`). If it's been red for > 2 weeks, escalate.

Then **squash-merge** to `main`. Never push to `main` directly.

## Phase 1 — UAT deploy and smoke (~30 min hands-on)

Deploy:

```bash
# Self-contained tester stack
make uat-up

# Or to a real UAT cluster
helm upgrade --install cooker deploy/helm/cooker \
  -n cooker-uat --create-namespace \
  -f your-uat-values.yaml
```

> For a hosted UAT cluster on AWS (EKS Auto Mode) with the SPA on Vercel, see
> [`DEPLOY-AWS-VERCEL.md`](DEPLOY-AWS-VERCEL.md) — it ships per-tier Helm
> overlays under `deploy/aws/values/` you can pass to `-f` here.

Run these 7 smoke checks in order. Each maps to a specific resilience guarantee shipped in PR #21.

### 1. Boot survives a missing IdP

```bash
COOKER_OIDC_ENABLED=true COOKER_OIDC_ISSUER_URL=https://does-not-exist.example.com \
  make uat-up
curl -fsS http://localhost:8080/health/live      # → 200
curl -i  http://localhost:8080/api/v1/pipelines  # → 503 + Retry-After: 30
```

Then point `COOKER_OIDC_ISSUER_URL` at a real IdP (no restart) and confirm the same `curl` returns 200. **Pass = no IdP outage takes Cooker out of service at boot.**

### 2. Pipelines actually execute end-to-end

Create a trivial pipeline in the UI; click Run. Watch the row transition `pending → running → success/failed`. **Pass = the row never gets stuck at `pending`** (this was the latent bug PR-3 closed).

### 3. Heartbeat advances during a long run

```bash
psql -c "SELECT id, status, heartbeat_at FROM pipeline_runs WHERE status='running';"
# Re-run after 30s; heartbeat_at should advance.
```

**Pass = `heartbeat_at` ticks every ~30s while running.**

### 4. Orphan sweep on hard restart

Start a long-running pipeline. While it's `running`:

```bash
docker kill -s KILL cooker-uat                   # or: kubectl delete pod cooker-... --grace-period=0
docker compose -f docker-compose.uat.yml up -d   # or: wait for K8s to restart the pod
psql -c "SELECT id, status, error FROM pipeline_runs WHERE id='<the run id>';"
```

**Pass =** row is `failed` with `error LIKE 'orphaned:%'` after restart. UI no longer shows the run as `running` forever.

### 5. Graceful shutdown drains in-flight requests

In one terminal, start a slow request (any pipeline-run trigger):

```bash
time curl -fsS -X POST http://localhost:8080/api/v1/pipelines/<id>/run \
  -H "Authorization: Bearer $TOKEN"
```

In another terminal, send SIGTERM:

```bash
docker kill -s TERM cooker-uat                   # or: kubectl rollout restart deploy/cooker
```

**Pass =** the in-flight request gets a response (not a connection-reset), and the binary exits within `terminationGracePeriodSeconds` (60s default).

### 6. `/health/ready` flips to 503 when a dependency is down

```bash
docker stop cooker-postgres
curl -i http://localhost:8080/health/ready       # → 503 with {"checks":{"db":"err: ..."}}
docker start cooker-postgres
sleep 5
curl -i http://localhost:8080/health/ready       # → 200
```

**Pass =** orchestrator can detect degraded state without restarting the pod.

### 7. Multi-replica WS broadcast crosses replicas

```bash
# K8s
kubectl scale -n cooker-uat deploy/cooker --replicas=2
# Compose: scale via `docker compose up --scale cooker=2`
```

Open the UI in two browser sessions (each will land on a different replica via the load balancer). Trigger a pipeline run from one. **Pass =** log lines stream in *both* browsers in real time.

If any of these 7 fail, **stop**. File a bug, fix on a follow-up branch, re-soak in UAT. Don't promote a known regression.

---

## Phase 2 — UAT bake (≥ 1 week)

Let UAT take realistic load for at least a week. Watch these signals daily:

| Signal | Healthy | Action if not healthy |
|---|---|---|
| `cooker_db_connection_errors_total` | 0 over 24h | Investigate Postgres before promoting |
| `cooker_redis_connection_errors_total` | 0 over 24h in steady state | Investigate Redis (network, eviction) |
| `cooker_jwks_fetch_failures_total` | 0 unless IdP genuinely went down | Confirm cluster egress to IdP |
| `cooker_pipeline_runs_orphaned_total` | 0 on a clean week | Pods are crashing — find root cause |
| `container_memory_working_set_bytes` | Flat over 24h | Linear growth = leak; profile via `pprof` |
| `go_goroutines` | Bounded; grows with concurrent runs and shrinks when they finish | Linear growth without runs = leak |
| `cooker_http_requests_total{code=~"5.."}` | < 0.1 % of total | Investigate the failing route |

The `RUNBOOK.md` "Recommended Alertmanager rules" appendix has PromQL for the first four.

**No promotion to production until all metrics are healthy across a full week.** Anything weirder is a bug; fix it in UAT first.

---

## Phase 3 — Apply perf follow-ups (optional but recommended)

Before production, ship the two highest-impact code follow-ups from `backlog.md` if they apply to your deployment:

- **`P0.1` OIDC atomic fast path** (~30 LOC). Removes a global mutex from the auth hot path. **Required if** you expect > 100 concurrent users.
- **`P0.2` Redis WS subscriber restart on disconnect** (~20 LOC). Without this, a Redis blip silently kills broadcasts forever per replica until pod restart. **Required if** Redis is multi-AZ or otherwise prone to brief disconnects.

Ship these as a small focused PR, soak in UAT for another 24h, then proceed.

The other `P0.*` items (`P0.3`-`P0.5`, `P0.7`) are micro-optimizations or correctness improvements — don't gate the cutover on them. `P0.6` is the OCI conformance workflow scope and doesn't affect runtime.

---

## Phase 4 — Production cutover

### Pre-flight checklist (operator-side; the chart can't decide these for you)

- [ ] **TLS on ingress** — cert-manager (or equivalent) issuing a valid cert for the production hostname. Reference it in `ingress.tls`. The chart refuses to render with `cookerEnv=production AND oidc.enabled=true AND ingress.enabled=true AND ingress.tls is empty`.
- [ ] **Postgres SSL** — `database.host` points at a TLS-capable Postgres; chart renders `?sslmode={{ .Values.postgresql.sslMode }}` (default `require`).
- [ ] **OIDC IdP** — production realm/tenant exists. Redirect URIs include the production hostname. `oidc.clientSecretRef.{name,key}` points at a pre-existing Kubernetes Secret holding the client secret.
- [ ] **Redis** — managed Redis (Elasticache, MemoryStore, in-cluster Redis with persistence off). The WS pub/sub doesn't need durable storage. `redis.url` set in values.
- [ ] **Replicas ≥ 2** — `replicaCount: 2` minimum so a pod restart doesn't blank-screen the UI. PodDisruptionBudget `minAvailable: 1`.
- [ ] **Multi-replica safety** — chart defaults flip `wsHub.backend / wsTicket.backend / rateLimit.backend` to `redis`. Verify they're not overridden to `memory` in your production values. `Config.Validate` refuses boot otherwise.
- [ ] **Audit destination** — `COOKER_AUDIT_DESTINATION=stdout` (default) routes via cluster log stack. Confirm the log shipper (Loki / Datadog / Splunk) is ingesting Cooker pod stdout.
- [ ] **Alertmanager rules** — paste the 4 rules from `RUNBOOK.md` "Recommended Alertmanager rules" into your Alertmanager config. Test by inducing a failure (scale Postgres to 0 for 30s; confirm `CookerDBConnectionErrors` fires).
- [ ] **Rollback plan reviewed** — see Phase 5 below.
- [ ] **Quiet maintenance window** — not Friday 17:00; not during a known traffic spike.

### Cutover steps

```bash
# 1. Dry-run the upgrade — review the diff before applying.
helm upgrade cooker deploy/helm/cooker \
  -n cooker -f production-values.yaml --dry-run | less

# 2. Apply for real.
helm upgrade cooker deploy/helm/cooker \
  -n cooker -f production-values.yaml

# 3. Watch readiness from outside the cluster for 5 minutes.
watch -n 5 'curl -fsS https://cooker.example.com/health/ready | jq'
# Expect status=ok, all checks ok.

# 4. Watch pods.
kubectl get pods -n cooker -w
# Expect: rolling restart completes; no CrashLoopBackOff.

# 5. Trigger a canary pipeline run via the UI.
# Confirm it executes end-to-end, log streaming works.

# 6. 5xx rate over 15 min.
# In your dashboard: rate(cooker_http_requests_total{code=~"5.."}[5m])
# Expect: ~0.
```

If any step looks wrong, **roll back immediately** (Phase 5). Don't try to fix forward under pressure.

---

## Phase 5 — Rollback plan

### Code/config rollback

Single-line:

```bash
helm rollback cooker <previous-revision> -n cooker
```

Find the previous revision with `helm history cooker -n cooker`.

### Schema rollback

The `006_run_heartbeat.up.sql` migration is **additive only** — adds a nullable column and a partial index. Old code that doesn't know about `heartbeat_at` reads the table fine. **No down-migration needed.** Future migrations should follow the same additive pattern.

If you ever need to undo the column (e.g., a future migration depends on its absence):

```sql
ALTER TABLE pipeline_runs DROP COLUMN heartbeat_at;
DROP INDEX IF EXISTS idx_pipeline_runs_running_heartbeat;
```

### Branch-level rollback

Revert the squash commit on `main`, redeploy. Code-level rollback is safe because nothing in PR #21 broke a public API.

### When NOT to roll back

- A single transient 5xx during the cutover. Wait 60s; it's likely the pod still spinning up.
- `cooker_jwks_fetch_failures_total` > 0 for a few seconds. Lazy OIDC discovery retries every 30s.
- One pipeline run failing post-cutover with a deploy-target error. Investigate that pipeline, not the platform.

Roll back if any of these:

- 5xx rate sustained > 1 % over 5 min.
- `/health/ready` returning 503 from > half the pods for > 5 min.
- `cooker_pipeline_runs_orphaned_total` jumping by > 5 within an hour (means pods are crashing).
- Authenticated requests returning 503 *after* the IdP is verified reachable.

---

## Phase 6 — Post-cutover improvements

Once production is stable for ~2 weeks, address the remaining backlog:

| Item | Effort | When |
|---|---|---|
| `P0.3` `time.NewTimer` in DB backoff | ~5 LOC | Any time |
| `P0.4` parallel readiness checks | ~15 LOC | Only if probe timeouts observed |
| `P0.5` binary WS broadcast encoding | ~50 LOC | Only after profiling shows it's hot |
| `P0.6` OCI conformance workflow scope | depends on log access | Once a human can paste a log from a `workflow_dispatch` run |
| `P0.7` OCI image-spec schema validation | ~half day | After distribution-spec conformance is green and stable |
| `P9.4` Tailscale `tsnet` transport | blocked on Go 1.26 GA | When toolchain catches up |
| `P9.2` cloud-target end-to-end against real accounts in CI | ~1 day per target | When customers actually use that target |

Anything new that's discovered during UAT / production gets filed as a backlog item, not patched directly into production.

---

## What this doc does not cover

- **Disaster recovery** (DB restore from backup, region failover). Covered separately by your infra team; Cooker just needs DB connectivity to come back.
- **Capacity planning** (replica count vs concurrent users). Start at 2 replicas; scale on `cooker_http_requests_total` rate.
- **Cost** — depends on your cluster, registry, and Redis choices.

If you find a gap in this doc during a real cutover, file it as a follow-up to `P0.8` so the next operator has a better playbook.
