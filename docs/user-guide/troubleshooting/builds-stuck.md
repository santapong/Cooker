# Builds stuck

A run starts, makes progress, then one stage stays in `running` indefinitely. The live log stream stops emitting. The UI shows the stage blue.

Ordered by frequency.

## 1. The container's entrypoint never exits

Most common cause for Build / Test / Custom stages.

**Symptom:** Logs show build output that completes successfully, but the stage doesn't transition to `success`.

**Cause:** The image you're running has a default entrypoint that doesn't exit. Common offenders:

- Test images with `tail -f /dev/null` or `sleep infinity` in their default CMD.
- Web-server images (`nginx`, `httpd`) that fork into the foreground.
- Custom images with `ENTRYPOINT ["sh"]` and no CMD — sh waits for input.

**Check:**

```sh
# Inspect the image you're running:
docker image inspect <image> | jq '.[0].Config.Cmd, .[0].Config.Entrypoint'
```

**Fix:** Set an explicit `command` in the stage config. For a Test stage:

```yaml
config:
  image: my-app:test
  command: ["sh", "-c", "go test ./..."]
```

## 2. Deploy waiting for a Pod that won't start

**Symptom:** Deploy stage. Logs show "deployment.apps/foo created" then nothing.

**Cause:** The deployed Deployment's pods are stuck — usually `ImagePullBackOff`, `CrashLoopBackOff`, or `Pending` due to insufficient resources.

**Check:**

```sh
kubectl get pods -n <target-namespace> -w
kubectl describe pod <pod-name> -n <target-namespace>
```

Common sub-causes:

- `ImagePullBackOff`: registry credentials missing in the target namespace, OR the just-pushed image hasn't propagated yet, OR the image path is wrong.
- `CrashLoopBackOff`: the container starts, then crashes. Read its logs: `kubectl logs <pod> -n <namespace>`.
- `Pending` with `0/N nodes are available`: no node satisfies the workload's `nodeSelector`, `affinity`, or resource requests.

**Fix:** address whatever `kubectl describe` reports. The Deploy stage will time out at the stage timeout (default `COOKER_RUN_DEADLINE`, often 30 min) and mark itself `failed` if the rollout doesn't complete.

## 3. Cooker pod restarted mid-run

**Symptom:** A run was making progress, you came back later, it's still showing `running`. Logs from the Cooker pod show a fresh start — the process clearly restarted.

**Cause:** The run's executor goroutine died with the pod. There's no automatic resume — runs don't recover across restarts. The heartbeat sweeper on the NEXT boot will mark it `failed` once the heartbeat is stale (`COOKER_RUN_HEARTBEAT_THRESHOLD`).

**Check:**

```sh
kubectl -n cooker get pod -o jsonpath='{.items[0].status.startTime}'
# Compare to the run's startedAt timestamp.

# In the DB:
SELECT id, status, heartbeat_at, NOW() - heartbeat_at AS heartbeat_age
FROM pipeline_runs WHERE status='running';
```

**Fix:** Wait for the next boot's sweep, OR restart the Cooker pod to trigger it immediately. Then re-run the pipeline / App deploy.

> **Known UX gap.** WebSocket reconnect is not automatic. If the Cooker container restarts mid-deploy, refresh the App detail page and trigger a new deploy. See [`docs/UAT.md`](../../guides/UAT.md#known-limitations-uat-compose).

## 4. Build Job pending in K8s

For Kaniko / Buildah builders:

**Symptom:** Build stage shows "submitting Kaniko Job" then nothing.

**Cause:** The Job's pod is `Pending` — same sub-causes as #2 (image pull, resource availability, RBAC).

**Check:**

```sh
kubectl -n cooker get jobs
kubectl -n cooker describe job <kaniko-job-name>
kubectl -n cooker get pods -l job-name=<kaniko-job-name>
```

**Fix:** Usually:

- **`ImagePullBackOff` on `gcr.io/kaniko-project/executor`**: the cluster can't reach gcr.io. Check egress.
- **`Forbidden` errors**: Cooker's ServiceAccount lacks the right RBAC. The chart provisions Job + Pod permissions automatically; namespace-scoped installs may need `kubectl get role -n cooker` to verify.
- **`MountVolume.SetUp failed for volume "context"`**: the `builder.kaniko.contextPVC` doesn't exist or doesn't allow `ReadWriteMany`.

## 5. WebSocket reading lag

**Symptom:** The browser tab's log panel stops scrolling, but the actual run is fine — refreshing the page shows the run completed.

**Cause:** The WS reader on the frontend fell behind, OR the ingress / proxy timed out the long-lived WS connection.

**Check:**

- Browser DevTools -> Network -> the WS connection. Look for "Disconnected" before run end.
- Run completion in the DB:
  ```sql
  SELECT status, started_at, finished_at FROM pipeline_runs WHERE id='<runId>';
  ```

**Fix:** Increase `proxy_read_timeout` (nginx) or equivalent on your ingress controller to at least 1 hour. See [WS disconnects](ws-disconnects.md).

## 6. The run deadline expired

**Symptom:** Stage status flips from `running` to `cancelled` (or `failed` with `error: run deadline exceeded`) without obvious cause.

**Cause:** Cooker has a global run deadline (`COOKER_RUN_DEADLINE`, default in code). Long ML or monorepo builds can exceed it.

**Check:** the run's error field. The run-level `error` says `run deadline exceeded` when this triggers.

**Fix:** Bump `COOKER_RUN_DEADLINE` cluster-wide. Per-Pipeline / per-App overrides do NOT exist yet — tracked as roadmap `D7`.

## 7. The run is rate-limited

**Symptom:** `POST /pipelines/:id/run` returns `429 Too Many Requests` immediately.

**Cause:** You're past the per-user rate limit (default 10 / minute, burst 3) on `POST /pipelines/:id/run`.

**Check:** The response `Retry-After` header tells you how long to wait.

**Fix:** Wait, or tune `COOKER_RATE_LIMIT_PER_MINUTE` / `_BURST`. In multi-replica, switch to `COOKER_RATE_LIMIT_BACKEND=redis` or expect false-positive blocks (the limiter is per-process).

## When all else fails — abort and re-run

Cancel the stuck run:

```sh
curl -X POST https://cooker.example.com/api/v1/pipelines/<ID>/runs/<RUN_ID>/cancel \
     -H 'Authorization: Bearer <jwt>'
```

The cancel propagates context deadlines to stage goroutines, which mostly causes their subprocesses to exit. Mostly — see [Runs: cancellation is cooperative](../concepts/runs.md#cancelling-a-run).

Then re-run the pipeline. If the same stage hangs again, the issue is in the stage's configuration, not in Cooker.

## Cross-references

- **[Runs](../concepts/runs.md)** — the heartbeat + orphan sweep model.
- **[Docker builds](../operations/docker-builds.md)** — builder selection and resource sizing.
- **[`docs/RUNBOOK.md`](../../guides/RUNBOOK.md)** — incident response for build-hung scenarios.
- **[WS disconnects](ws-disconnects.md)** — when the issue is the log stream, not the run itself.
