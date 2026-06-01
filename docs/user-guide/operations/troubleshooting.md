# Troubleshooting

The fifteen issues most commonly hit by Cooker operators, ordered by frequency. Each entry: symptom -> first thing to check -> common cause -> fix.

For dedicated runbooks of the worst symptoms, see the [Troubleshooting pages](../troubleshooting/). For incident response (DB down, OIDC unreachable, etc.), see [`docs/RUNBOOK.md`](../../guides/RUNBOOK.md).

## 1. "config: ... is required in production" on boot

**Symptom:** Cooker pod CrashLoopBackOff. Logs show `{"level":"ERROR","msg":"invalid configuration","err":"config: ..."}`.

**Cause:** `Config.Validate()` failed in production mode. Missing one of `DATABASE_URL`, `COOKER_SECRET_KEY`, `COOKER_ALLOWED_ORIGINS`, or you have `COOKER_BUILDER=docker`.

**Fix:** Read the full error string — it lists every problem. Set the missing vars in your Helm values / compose env. Restart.

See [Configuration: Validation](../getting-started/configuration.md#validation-production).

## 2. Sign-in loop / "Loading…" forever

**Symptom:** Land on Cooker, get redirected to IdP, redirected back to `/callback`, redirected to `/`, redirected to IdP. Loop.

**Cause:** Almost always one of:

- `COOKER_OIDC_REDIRECT_URL` doesn't match the IdP's registered redirect URI (including trailing slash).
- The frontend was built against a different `VITE_OIDC_*` set than the backend's `COOKER_OIDC_*`.
- The IdP's clock is more than 5 minutes off.

**Fix:** See [Login loop](../troubleshooting/login-loop.md).

## 3. Stage stuck in `running`

**Symptom:** A stage shows `running` status indefinitely. Live log stream stops emitting.

**Causes (ordered):**

1. The build / test container's entrypoint never exits. Common in compose-style images that `tail -f /dev/null` by default.
2. The deploy is waiting for a Deployment that's stuck in ImagePullBackOff or ContainerCreating.
3. The Cooker pod restarted mid-run — the WS reconnect path is not automatic. The run is in the orphan-sweep range and will get marked `failed` on the NEXT Cooker boot. Force a restart, or wait for the heartbeat to time out.

**Fix:** See [Builds stuck](../troubleshooting/builds-stuck.md).

## 4. `403 mfa_required` on Approve / Delete / Secret reveal

**Symptom:** Admin tries to delete a pipeline, approve a promotion, or reveal a secret. Gets `{"error":"mfa_required","acr_values":["mfa"]}`.

**Cause:** `COOKER_OIDC_MFA_ACR_VALUES` is configured, the user's JWT doesn't carry an acceptable `acr` claim.

**Fix:** The frontend should auto-redirect to sign-in with `acr_values=<configured>`. If it doesn't (or you're calling the API directly), sign out and back in via the IdP's MFA flow. The new JWT will carry the required claim.

## 5. WebSocket disconnects mid-run

**Symptom:** Run starts streaming logs, then the stream stops. Refreshing the page shows the run still running with full server-side logs.

**Causes:**

- Ingress / proxy timed out the long-lived connection. Default for most controllers is 60-90s of idle.
- WS ticket expired (60s) before the upgrade; the new auto-retry doesn't always trigger.
- A second tab / browser stole the ticket (tickets are single-use; first redemption wins).

**Fix:** See [WS disconnects](../troubleshooting/ws-disconnects.md). For nginx, the magic incantation is `proxy_read_timeout 86400` on the WS-handling location.

## 6. Build fails with "no such file or directory: Dockerfile"

**Symptom:** Build stage fails immediately with this error.

**Cause:** Either the `dockerfile` field in the stage config doesn't match the actual path in the repo, OR the `context` is wrong (Cooker is looking in the wrong subdirectory).

**Fix:** Verify:

- The repo's actual Dockerfile path (`git ls-files | grep -i dockerfile`).
- The stage's `config.dockerfile` matches.
- The `config.context` is the directory the Dockerfile lives in (or its parent).

## 7. Push fails with `denied: requested access to the resource is denied`

**Symptom:** Push stage fails with this OCI error.

**Cause:** Registry credentials missing, wrong, or expired. ECR auth tokens last 12 hours; if you're rotating via cron, the cron may have failed.

**Fix:** See [Registries](../guides/registries.md) for per-registry credential setup. Verify with:

```sh
docker login <registry>     # locally, with the same creds
```

If `docker login` works but Cooker fails, the credentials aren't being matched. Cooker matches by URL prefix — make sure the registry URL prefix in `RegistryConfig` covers the stage's `registry` field.

## 8. Deploy fails: "the server doesn't have a resource type \"Deployment\""

**Symptom:** Deploy stage fails with this error from `kubectl`.

**Cause:** Wrong kubeconfig context, or the API server isn't reachable.

**Fix:**

- Verify `KUBECONFIG` is pointing at the right file.
- Verify the right context: `kubectl --kubeconfig=$KUBECONFIG config current-context`.
- For in-cluster: verify the ServiceAccount token is mounted (`ls /var/run/secrets/kubernetes.io/serviceaccount/`).

## 9. Pipeline edit fails with `409 Conflict`

**Symptom:** Two operators editing the same pipeline; the second to save gets 409.

**Cause:** Optimistic concurrency. `Pipeline.Version` is the token; if your PUT echoes a stale version, the server rejects to prevent silent clobbering.

**Fix:** Reload the pipeline in the UI; you'll see the other operator's changes. Re-apply yours and save again.

Same applies to `App.Version`, `Environment.Version`, `Host.Version`.

## 10. Postgres "advisory lock not available" hang

**Symptom:** Cooker pod logs "applying migrations" and never finishes.

**Cause:** A previous Cooker pod crashed while holding `pg_advisory_lock(847263)`. Postgres held the lock for the dead session.

**Fix:** Connect with `psql` to the same database:

```sql
SELECT pid, locktype, mode, granted
FROM pg_locks
WHERE locktype = 'advisory';

-- find the dead session, then:
SELECT pg_advisory_unlock(847263);
```

Restart the Cooker pod. The lock will reacquire cleanly.

See [pg-migration-errors](../troubleshooting/pg-migration-errors.md).

## 11. `503 Service Unavailable` from the secrets API

**Symptom:** `PUT /api/v1/environments/:id/secrets/:key` returns 503.

**Cause:** `COOKER_SECRET_KEY` is missing or invalid (database backend) — the Codec isn't initialised, so any encrypt/decrypt call fails closed.

**Fix:** Set `COOKER_SECRET_KEY` to a base64-encoded 32-byte key. Restart.

```sh
head -c 32 /dev/urandom | base64
```

> **Warning.** Once you set the key, do NOT rotate it without a one-shot re-encrypt step. The current Codec has no dual-key path (`S26-05-08`). Rotating loses every previously sealed secret in the database.

## 12. CORS errors in the browser console

**Symptom:** Browser console shows `Access to fetch at ... has been blocked by CORS policy`.

**Cause:** `COOKER_ALLOWED_ORIGINS` doesn't include the origin the browser is using.

**Fix:** Add the exact origin (including scheme and port) to the CSV. Restart. Note that production refuses `*` — list explicit origins.

## 13. `401 Unauthorized` from `/api/v1/...`

**Symptom:** Authenticated user gets 401 on every API call.

**Causes (ordered):**

1. The JWT expired and the auto-refresh failed (refresh token expired too).
2. The IdP rotated its JWKS and Cooker's cache is stale. Cooker re-fetches lazily, but the in-flight request fails.
3. Clock skew between Cooker and the IdP (> 5 minutes).

**Fix:** Sign out and back in. If many users hit this simultaneously, check the IdP's signing key rotation and Cooker's logs for `oidc: token verify failed`.

## 14. App stays `unknown` health

**Symptom:** Deployed App's health status sits at `unknown` indefinitely.

**Causes:**

- `COOKER_APP_HEALTH_INTERVAL=0` (checker disabled).
- The App's deploy target has no probe wired (Cloud Run / ECS / Fly / Render probers are partial).
- The K8s Deployment hasn't created any pods yet.

**Fix:** Check `kubectl get deploy <app-name>`. If Available=1, Cooker should show `healthy` within `COOKER_APP_HEALTH_INTERVAL` seconds (default 30).

## 15. Helm `helm install` fails with "no matches for kind..."

**Symptom:** Initial install fails with errors about `ServiceMonitor`, `NetworkPolicy`, or `PodDisruptionBudget`.

**Cause:** The cluster doesn't have the relevant CRD or API group enabled.

**Fix:**

- `ServiceMonitor` — needs Prometheus Operator. Either install it, or set `serviceMonitor.enabled=false`.
- `NetworkPolicy` — needs a CNI that supports them (Calico, Cilium, k3s with `--flannel-backend=none`, etc.). Set `networkPolicy.enabled=false` if your cluster doesn't.
- `PodDisruptionBudget` — only available on K8s 1.21+. Older clusters: drop the chart and rebuild without it.

## Cross-references

- **[`docs/RUNBOOK.md`](../../guides/RUNBOOK.md)** — incident response for the worst-case symptoms (DB down, IdP outage, OOM).
- **[Login loop](../troubleshooting/login-loop.md)** — dedicated walkthrough.
- **[Builds stuck](../troubleshooting/builds-stuck.md)** — dedicated walkthrough.
- **[WS disconnects](../troubleshooting/ws-disconnects.md)** — dedicated walkthrough.
- **[pg-migration-errors](../troubleshooting/pg-migration-errors.md)** — dedicated walkthrough.
