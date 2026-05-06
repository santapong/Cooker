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

---

## Recommended Alertmanager rules

These four counters are exposed on `/metrics` (when `COOKER_METRICS_ENABLED=true`). Each stays at zero on a healthy deployment; sustained `rate > 0` over 5 minutes indicates degradation worth paging on.

```yaml
groups:
  - name: cooker-resilience
    rules:
      - alert: CookerDBConnectionErrors
        expr: rate(cooker_db_connection_errors_total[5m]) > 0
        for: 5m
        labels: { severity: page }
        annotations:
          summary: "Cooker is failing to reach Postgres"
          runbook: "docs/RUNBOOK.md#postgresql-is-down-or-unreachable"

      - alert: CookerRedisConnectionErrors
        expr: rate(cooker_redis_connection_errors_total[5m]) > 0
        for: 5m
        labels: { severity: page }
        annotations:
          summary: "Cooker Redis backend (rate limit / WS ticket / WS hub) is erroring"

      - alert: CookerJWKSFetchFailures
        expr: rate(cooker_jwks_fetch_failures_total[5m]) > 0
        for: 10m
        labels: { severity: warn }
        annotations:
          summary: "Cooker cannot reach the OIDC IdP for JWKS"
          runbook: "docs/RUNBOOK.md#oidc-issuer-is-unreachable"

      - alert: CookerOrphanedRunsHigh
        # rate of orphans means pods are crashing under load
        expr: increase(cooker_pipeline_runs_orphaned_total[1h]) > 5
        for: 0m
        labels: { severity: warn }
        annotations:
          summary: "Cooker swept >5 orphaned runs in the last hour"
          runbook: "docs/RUNBOOK.md#recovery-after-restart"

      - alert: CookerAuditEventsDropped
        # T16's async file sink drops events when the queue fills
        # (typically: disk full, or a sustained-slow disk). One drop
        # is informational, sustained drops mean the audit trail is
        # incomplete.
        expr: increase(cooker_audit_events_dropped_total[10m]) > 0
        for: 10m
        labels: { severity: warn }
        annotations:
          summary: "Cooker audit sink dropping events (disk full?)"
          runbook: "docs/RUNBOOK.md#audit-sink-dropping-events"

      - alert: CookerStageDurationHigh
        # The histogram's p95 over 5m. Tune the threshold per stage
        # type (build is naturally slower than push); 30 min here is
        # a generous catch-all.
        expr: histogram_quantile(0.95, sum by (le, type) (rate(cooker_pipeline_stage_duration_seconds_bucket[5m]))) > 1800
        for: 15m
        labels: { severity: warn }
        annotations:
          summary: "Pipeline stage p95 duration above 30 min"
          runbook: "docs/RUNBOOK.md#stages-are-slow"
```

---

## Audit sink dropping events

**Symptom:** `cooker_audit_events_dropped_total` is incrementing; logs include `audit: file sink overflow; events dropped`.

**Cause:** T16 turned the audit file sink async with a bounded queue (1024) and drop-on-full so a slow / full disk can no longer freeze every authenticated request behind a held mutex. Dropped events are *gone*; the trade-off is operator-visible: prefer dropping a few events to wedging the API.

**Mitigation:**

1. Check disk usage on the audit volume (`COOKER_AUDIT_FILE_PATH`'s parent). The most common cause is the audit log filling the volume.
2. Rotate the audit log (`logrotate(8)` with `copytruncate`) and ensure rotation is automated.
3. If sustained, route audit to a remote sink (stdout → fluent-bit → SIEM) rather than a local file — `COOKER_AUDIT_DESTINATION=stdout`.

---

## Stages are slow

**Symptom:** `cooker_pipeline_stage_duration_seconds` p95 is above the threshold for one or more stage types.

**First checks:**

- `kubectl describe job -n cooker-builders <jobname>` for Kaniko / Buildah Pods that are stuck.
- Registry side: a slow `docker push` is the most common cause. Test from inside the cluster: `crane push small-image registry/$REPO:tt`.
- Postgres / Redis latency from the readiness probe — if probes are slow, every status update is slow.

**Mitigation:** stages now respect `Stage.Config.Timeout` (T10), so once a slow stage exceeds its timeout it fails cleanly and the run resumes / marks failed. If a customer wants more retries, raise `Stage.Config.Retries`.

---

## Backup, retention, restore

Cooker keeps everything operationally significant in PostgreSQL: pipelines, runs, environments, apps, hosts, users, schema_migrations, and the embedded run history (JSONB on `pipeline_runs`). Lose the database and you lose history; the schema can be re-created from an empty database via the embedded migrations.

### Backup

The chart does **not** ship a backup operator. Pick one of:

- **Bitnami's `postgresql` chart with `backup.enabled=true`** (uses `pg_basebackup` to S3-compatible storage). Simplest if Cooker's Postgres is co-installed.
- **Velero** with the `csi-snapshotter` plugin — block-level snapshot of the PVC.
- **External managed Postgres** (RDS, Cloud SQL, Aiven). All of them support point-in-time-restore via WAL; turn it on.

For self-hosted: a daily `pg_dump --format=custom > /backups/cooker-$(date +%F).dump` with a 30-day retention is the minimum. Ship to off-site storage; do not keep backups on the same node.

### Retention

Without intervention, `pipeline_runs` grows without bound. Schedule a CronJob:

```sql
DELETE FROM pipeline_runs
 WHERE status IN ('success','failed','cancelled')
   AND finished_at < NOW() - INTERVAL '90 days';
```

`pipelines` cascades on delete to `pipeline_runs`, so deleting a pipeline already deletes its runs. Tune the 90-day window per the audit trail your environment requires.

### Restore drill

Practice restore at least quarterly. The drill:

1. `kubectl scale deployment/cooker --replicas=0` in the target namespace (so no writes hit the new database while it's catching up).
2. `pg_restore --clean --if-exists --no-owner -d "$DATABASE_URL" /backups/cooker-YYYY-MM-DD.dump`
3. Run `cooker generate-key` if `COOKER_SECRET_KEY` was lost — environment secrets sealed under the old key cannot be opened. (T19's dual-key support is on the long-term roadmap; today, key loss = secrets loss.)
4. `kubectl scale deployment/cooker --replicas=N` and verify `/health/ready` is green and `/version` reflects the running build.
5. Smoke-test: list pipelines, fetch one run, trigger a no-op pipeline.

If restore takes longer than 1 hour, your backup format is wrong (use `--format=custom`, not `--format=plain`).

---

## On-call escalation

The page-worthy alerts above route to the platform on-call. Suggested escalation:

| Tier | Time-to-engage | Owner |
|------|----------------|-------|
| L1 | 0–15 min | Platform on-call (PagerDuty rotation) |
| L2 | 15–60 min | Cooker maintainer team (`#cooker-eng` Slack) |
| L3 | 60+ min | Engineering leadership |

For data-loss incidents (audit dropped events sustained > 1h, Postgres restore from backup, secret-key loss), engage the security / compliance team in parallel with L2 — those are usually reportable.

---

## Secrets-backend failure modes

The configured `COOKER_SECRETS_BACKEND` decides what fails when secret retrieval errors:

| Backend | Failure mode | Mitigation |
|---------|--------------|------------|
| `database` (default) | Cooker's own Postgres is the secret store. If Postgres is down, secret reveal returns 500. | Same Postgres recovery as the rest of the app. |
| `keepsave` | KeepSave HTTPS endpoint unreachable → 5xx on reveal; `cooker_secrets_keepsave_errors_total` increments (if exposed). T19 ensures TLS is at least 1.2 and the URL is `https://`. | Confirm KeepSave server health; cooker doesn't fall back to a local copy. |
| `vault` | Vault token expiry / sealed Vault → reveal fails. | Renew the AppRole token; check `vault status`. |
| `aws` (Secrets Manager) | IAM role missing `secretsmanager:GetSecretValue` → 403. | Check the cooker pod's IRSA role and the secret's resource policy. |
| `gcp` (Secret Manager) | Service-account JSON missing `secretmanager.secretAccessor` → 403. | Check Workload Identity binding and the GSA's secret-level IAM. |

In every case the cooker pod logs the underlying error at `WARN`/`ERROR`; clients see a generic "secret backend error" (T20).

---

## Monitoring dashboards

Pre-built dashboards live in `deploy/observability/dashboards/` (Grafana JSON, Prometheus rules). Key panels:

- **HTTP request rate / latency** (`cooker_http_request_duration_seconds`) — split by route template.
- **Stage duration** (`cooker_pipeline_stage_duration_seconds`, T18) — split by `type` and `status`.
- **Resilience counters** — `cooker_db_connection_errors_total`, `cooker_redis_connection_errors_total`, `cooker_jwks_fetch_failures_total`, `cooker_audit_events_dropped_total`, `cooker_run_heartbeat_errors_total`, `cooker_pipeline_runs_orphaned_total`.
- **Build SHA** — scrape `/version` into a `build_info` gauge per replica so the dashboard's title shows what's running.
