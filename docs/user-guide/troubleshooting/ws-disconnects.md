# WebSocket disconnects

The run starts, logs stream live for a while, then the stream stops. Refreshing the page shows the run finished long ago; you just stopped seeing it.

Ordered by frequency.

## 1. Ingress / proxy idle timeout

Most common. Default idle timeouts:

| Controller | Default |
|---|---|
| nginx-ingress | 60s |
| AWS ALB | 60s |
| GCP LB | 30s (HTTP), longer for WebSocket-aware classes |
| Traefik | 0 (no limit) — but check your `entryPoints` config |
| Envoy | 60s |

A long build (5+ minutes) easily exceeds these. The ingress drops the connection silently from the client's perspective.

**Check:**

- Browser DevTools -> Network -> WS connection. "Disconnected" timestamp tells you exactly when.
- Compare against the run's `started_at` + the configured timeout.

**Fix:** raise the timeout on the WS path. For nginx-ingress, the annotation:

```yaml
metadata:
  annotations:
    nginx.ingress.kubernetes.io/proxy-read-timeout: "86400"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "86400"
```

For ALB, set `idle_timeout.timeout_seconds` on the target group. For Traefik, set `transport.lifeCycle.requestAcceptGraceTimeout`. For Envoy, set `stream_idle_timeout`.

[`docs/MULTI_REPLICA.md`](../../MULTI_REPLICA.md) has the full per-controller annotation snippets.

## 2. WS ticket already consumed

WS tickets are single-use. If you open the same run's log channel in two tabs, the second one will get `401 Unauthorized` because the ticket the SPA fetched was already redeemed by the first.

**Symptom:** New tab can't see live logs even though it loaded fine.

**Check:** Browser DevTools -> Network -> the `POST /api/v1/ws-tickets` request and the subsequent WS upgrade. Look for `401` on the WS endpoint.

**Fix:** The SPA should request a fresh ticket per WS connection. If it's reusing one, that's a frontend bug — file an issue.

Workaround: close the original tab; the new one can request its own ticket.

## 3. Multi-replica with memory-backed WS state

Without sticky sessions or Redis-backed state, the ticket issued by replica A is unknown to replica B. The WS upgrade lands on B (because Service load-balances), B doesn't have the ticket, returns 401.

**Check:** Are you running `replicaCount > 1`?

```sh
kubectl -n cooker get deploy cooker -o jsonpath='{.spec.replicas}'
```

If yes, `Config.Validate()` should have refused boot with memory backends. If it didn't, you might be on `cookerEnv: dev|uat` where the validation is lenient.

**Fix:** Set `COOKER_STICKY_SESSIONS=true` and configure ingress affinity, OR switch backends to Redis:

```bash
COOKER_RATE_LIMIT_BACKEND=redis
COOKER_WS_TICKET_BACKEND=redis
COOKER_WS_HUB_BACKEND=redis
```

See [Self-hosting: multi-replica](../guides/self-hosting-tips.md#multi-replica).

## 4. CSP blocking the WS upgrade

**Symptom:** Browser DevTools console shows `Refused to connect to 'wss://...' because it violates the following Content Security Policy directive`.

**Cause:** The CSP doesn't include `wss:` in `connect-src`.

**Fix:** Update the CSP at the ingress / reverse proxy to include `wss:`:

```text
Content-Security-Policy: default-src 'self'; connect-src 'self' wss:;
```

The chart's middleware adds this by default; ingress-level CSPs may override it.

## 5. Cooker pod restarted mid-stream

**Symptom:** Stream stops abruptly; in Cooker logs, there's a new boot near the disconnect time.

**Cause:** The pod was OOMKilled, evicted, or restarted (rollout / config change).

**Check:**

```sh
kubectl -n cooker get pod -o jsonpath='{.items[0].status.startTime}'
kubectl -n cooker describe pod | grep -A5 "Last State"
```

If `Last State: Terminated` with reason `OOMKilled`, bump memory limits and check what the build was doing — large in-memory tarballs are a common culprit.

**Fix:**

- Bump memory limits.
- For the run itself: it might be salvageable if the heartbeat is still alive. Otherwise [trigger a re-run after the orphan sweep](builds-stuck.md#3-cooker-pod-restarted-mid-run).

## 6. Same-origin policy

**Symptom:** Browser console shows `Failed to construct 'WebSocket': An insecure WebSocket connection may not be initiated from a page loaded over HTTPS`.

**Cause:** The SPA was served over `https://` but constructs `ws://` (not `wss://`).

**Check:** Browser DevTools -> Network -> WS request URL.

**Fix:** This is a frontend bug — file an issue. The SPA should detect the scheme and choose `wss://` when on `https://`.

Workaround: open the SPA over `http://` (don't do this in production).

## 7. Ticket TTL expired before upgrade

Tickets are valid for 60 seconds. If the browser issues a ticket then waits more than 60s before opening the WS (rare, but possible on slow networks or paused tabs), the ticket has expired by the time it's used.

**Symptom:** Sporadic 401 on WS upgrade, only after long delays.

**Fix:** None needed at the operator level — the SPA should fetch a fresh ticket per WS open. If it isn't, file an issue.

## Diagnosing the right channel

Cooker has multiple WS channels:

| Channel | What |
|---|---|
| `/ws/pipeline-run/:runId` | Run-level status. |
| `/ws/app-run/:runId` | App-deploy run events. |
| `/ws/runs/:runId/stages/:stageId/logs` | Per-stage logs. |
| `/ws/docker/build/:buildId` | Manual Docker build output. |
| `/ws/kubernetes/watch?namespace=...&resource=...` | K8s watch fan-out. |

If the run-level channel works but the stage-log channel doesn't (or vice versa), narrow accordingly — they have separate handlers.

## Cross-references

- **[Self-hosting: multi-replica](../guides/self-hosting-tips.md#multi-replica)** — ingress affinity and Redis backends.
- **[`docs/MULTI_REPLICA.md`](../../MULTI_REPLICA.md)** — per-controller annotation snippets.
- **[Builds stuck](builds-stuck.md)** — when the issue is the run, not the stream.
