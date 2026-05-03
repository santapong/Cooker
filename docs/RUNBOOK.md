# Incident response runbook

What to do when Cooker is broken. Each section answers: **symptom → first checks → most-likely cause → mitigation**.

> Backlog reference: P8.

---

## Recovery after restart

**Symptom:** Several `pipeline_runs` rows show `status='failed'` with `error='orphaned: heartbeat stale at boot'` after a Cooker pod has restarted.

**Cause:** The pod was killed (OOM, SIGKILL past terminationGracePeriodSeconds, node failure) while runs were in flight. On every boot Cooker sweeps `pipeline_runs WHERE status='running' AND (heartbeat_at IS NULL OR heartbeat_at < NOW() - 90s)` and marks them failed so the UI no longer shows them as running forever.

**No action required** unless the count is high — a sustained orphan rate means pods are crashing under load. Check `cooker_pipeline_runs_orphaned_total` (Prometheus) and the pod's previous logs.

**Manual sweep**, if you want to force it without a restart:
```sql
UPDATE pipeline_runs
   SET status='failed', error='orphaned: manual sweep', finished_at=NOW()
 WHERE status='running' AND (heartbeat_at IS NULL OR heartbeat_at < NOW() - interval '90 seconds');
```

---

## Build runs forever

**Symptom:** A pipeline run shows `running` long past the configured stage timeout. WebSocket log stream is silent or repeats the last line.

**First checks:**
1. `kubectl get pods -n cooker` — is the Cooker pod healthy? OOMKilled?
2. `kubectl logs -n cooker deploy/cooker --tail 200` — any panic, deadlock, or stuck spawn?
3. If `COOKER_BUILDER=docker`: `docker ps -a` on the host running Cooker. Is a build container hung?
4. The DB: heartbeat freshness is the most useful signal:
   ```sql
   SELECT id, status, started_at, NOW() - heartbeat_at AS staleness
     FROM pipeline_runs WHERE status='running' ORDER BY started_at LIMIT 20;
   ```
   Staleness > 90s = orphan; the next boot's sweep will mark it failed automatically.

**Most-likely causes:**
- Build process exited but Cooker missed the SIGCHLD (rare; restart Cooker).
- Docker daemon hung on the host (`/var/run/docker.sock` deadlock). `systemctl restart docker` on the host clears it. **This is exactly the failure mode P1.1 (Kaniko) eliminates.**
- Subprocess fan-out: `docker build` spawned a long-running pull and Cooker's context didn't cancel. Workaround: cancel the run via API; root fix is the BuildKit gRPC adapter (P9.1).

**Mitigation:**
```bash
# Cancel the run cleanly
curl -X POST https://cooker.example.com/api/v1/pipelines/$PID/runs/$RID/cancel \
  -H "Authorization: Bearer $TOKEN"

# If that hangs too, restart the Cooker pod
kubectl rollout restart -n cooker deploy/cooker
```

The in-flight run will be marked `failed` on next state reconciliation.

---

## Probe semantics: live vs ready

`/health/live` is unconditional — it returns 200 as long as the Gin router is serving. Wire it to `livenessProbe` so a stuck process gets restarted.

`/health/ready` runs a 1-second DB ping, a Redis ping when Redis is configured, and (post PR-2) a JWKS-cache age check. Returns 503 with a per-check breakdown when any dep is down. Wire it to `readinessProbe` so a transient infra blip removes the pod from service without restarting it.

`/health` is kept as an alias for `/health/live` for backward compatibility.

---

## Rolling restart

`kubectl rollout restart deploy/cooker` triggers a graceful shutdown. The pod receives SIGTERM, drains in-flight HTTP requests for up to 30 seconds, finishes tracked pipeline runs for up to 25 seconds beyond that, then exits. `terminationGracePeriodSeconds: 60` in the chart leaves a small safety buffer; bump it if you regularly run pipelines longer than 30 seconds and want more drain time.

If the pod takes longer than the grace period, K8s SIGKILLs it; in-flight runs are then surfaced as orphans by the next boot's sweep (see "Recovery after restart").

---

## PostgreSQL is down or unreachable

**Symptom:** API requests return `500` with body `{"error":"db: ..."}` or `connection refused`. `/health/ready` returns 503 with `db: err: ...`. `/health/live` keeps returning 200 — the pod is alive, it's just not ready to serve.

**First checks:**
1. `/health/ready` from outside: `curl -fsS https://cooker.example.com/health/ready`. Expect 503 with the failing dep named.
2. Postgres pod: `kubectl get pod -n cooker -l app.kubernetes.io/name=postgresql`.
3. Network path: `kubectl exec -n cooker deploy/cooker -- nc -zv cooker-postgresql 5432`.
4. Connection saturation: `SELECT count(*) FROM pg_stat_activity WHERE datname='cooker';`
5. `kubectl logs -n cooker statefulset/cooker-postgresql --tail 100`.

**Boot-time blips no longer crash the pod.** The connection logic at startup retries with jittered exponential backoff up to 5 minutes (delay 0.5s → 30s capped). A pod restarted during a Postgres rolling upgrade should reconnect on its own.

**Most-likely causes:**
- StatefulSet PVC ran out of space — logs show `could not extend file`.
- Connection saturation — typical when the PgBouncer pool is undersized or Cooker is leaking connections.
- Failover lag if you run a managed HA Postgres.

**Mitigation:**
- Out-of-space: scale the PVC, restart the pod. Plan a Postgres-side rotation policy for `pipeline_runs` if this is recurring.
- Connection saturation: temporarily lower Cooker `replicaCount` to 1, restart, investigate. Long-term: pgbouncer in front of Postgres.
- For a planned Postgres outage, set Cooker `replicaCount: 0` first — better than thrashing healthchecks.

---

## OIDC issuer is unreachable

**Symptom:** All sign-in attempts hang or return `502` from the IdP. New sessions can't authenticate; existing sessions keep working until their token expires.

**First checks:**
1. From inside the cluster: `kubectl exec -n cooker deploy/cooker -- wget -O- $COOKER_OIDC_ISSUER_URL/.well-known/openid-configuration` (or `curl`). If this fails, Cooker can't validate tokens regardless of UI.
2. Cluster DNS: `nslookup` the issuer hostname from the pod.
3. The IdP's status page (Auth0, Okta, Azure AD all publish one).
4. If you front the IdP with your own ingress, check that side first.

**Most-likely causes:**
- Cluster egress NetworkPolicy or firewall blocking the IdP.
- IdP outage.
- Issuer URL typo (only fails on token refresh; cached `.well-known` masks it for a while).

**Mitigation:**
- IdP outage: communicate, wait. Existing sessions keep working until access tokens expire (default 1h) and refresh fails. Cooker self-heals when the IdP returns — provider discovery is lazy and retried every 30s after each failure.
- A boot during an IdP outage no longer crash-loops the pod. The middleware accepts construction with an unreachable IdP; authenticated requests return `503` with `Retry-After: 30` until discovery succeeds.
- Egress block: temporarily widen the NetworkPolicy egress rule, then narrow back when the IdP DNS is verified.
- **Emergency bypass** (use with caution, audit log): set `COOKER_OIDC_ENABLED=false` and `COOKER_ENV=uat` and restart. The dev admin user will be injected on every request. Revert as soon as the IdP is back. Log the timeline in the audit channel.

---

## Secrets backend (KeepSave) is unreachable

**Symptom:** `RevealSecret` returns `500`, `PutSecret` returns `500`. Pipeline runs that hydrate env from secrets fail at the resolution stage. Other endpoints unaffected.

**First checks:**
1. KeepSave pod / external service: `kubectl exec -n cooker deploy/cooker -- nc -zv $KEEPSAVE_HOST 8080`.
2. Cooker logs — the KeepSave HTTP client wraps non-2xx as `keepsave: <code> <message>`; grep for that prefix.
3. KeepSave's own logs / status.

**Most-likely causes:**
- KeepSave pod restarted, taking longer than 30s (the client timeout) to come back.
- API key was rotated on KeepSave but not updated in Cooker.
- KeepSave's circuit breaker tripped (5 consecutive 5xx within 30s opens the breaker for 30s).

**Mitigation:**
- Restart KeepSave; wait for it to settle.
- Rotate the Cooker-side API key reference and bounce Cooker.
- If KeepSave is the source of truth and unrecoverable, you have an outage — there is no DB fallback once `COOKER_SECRETS_BACKEND=keepsave` (system of record). Backups must come from KeepSave's own backup story.

---

## High memory / OOMKilled

**Symptom:** Pod restarts in a loop, `kubectl describe pod` shows `Last State: Terminated, Reason: OOMKilled`.

**First checks:**
1. `kubectl top pod -n cooker`.
2. Recent build activity — did someone trigger a parallel build of a large image with `COOKER_BUILDER=docker`?
3. Goroutine count — Cooker exposes pprof in dev only; in production, consider enabling `/debug/pprof` behind admin auth (open backlog item).

**Most-likely causes:**
- WebSocket connections accumulating without being closed (frontend bug, see backlog P5 "WS auto-reconnect").
- Subprocess fan-out from CLI fallbacks (each `docker build`/`docker push`/`kubectl apply` allocates).

**Mitigation:**
- Bump `resources.limits.memory` in `values.yaml` and redeploy.
- Restart the pod to drop accumulated state.
- Long-term: P9.1 (native SDK rewrites) removes the subprocess fan-out.

---

## Audit log is missing entries

**Symptom:** Operator inspects logs after an incident; mutating actions show up but actor / target / reason are unstructured or missing.

**Status:** Audit logging middleware is not yet implemented (backlog P1.2). Today, mutations log to stdout via the standard `log` package — unstructured and not parseable. Until P1.2 lands:

- Treat the existing logs as advisory, not authoritative.
- For audit-grade events, correlate with the IdP's access log + the Cooker DB row mtime.
- Schedule P1.2 as the next P1 alongside Kaniko.
