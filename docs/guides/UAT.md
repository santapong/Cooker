# Cooker — UAT Runbook

One command to start, a reachable URL, a working build+deploy
target, and clean teardown. Intended for testers exercising the
Apps "Deploy" button and filing bugs.

> **Status:** UAT-ready, not production-ready. Read the "What
> works" and "What's scaffolded" sections before you start so you
> know what to hit and what to skip.

> **Hosted UAT on Vercel + AWS:** to run UAT off your laptop — SPA on
> Vercel with per-PR previews, backend on AWS Lightsail — see
> [`DEPLOY-AWS-VERCEL.md`](DEPLOY-AWS-VERCEL.md).

## Prerequisites

- **Docker 24+** with the daemon running. On Linux, your user
  must be able to run `docker` without `sudo`.
- **`make`** and **~10GB free disk** / **~4GB free RAM**.
- **One-time host-daemon edit**: add the in-stack registry to
  Docker's insecure list, because Cooker pushes via the host
  daemon and that daemon refuses plaintext pulls/pushes by default.

  Edit `/etc/docker/daemon.json` (create it if missing):

  ```json
  { "insecure-registries": ["registry:5000"] }
  ```

  Then restart Docker (`sudo systemctl restart docker` on Linux,
  Docker Desktop → Restart on macOS/Windows). This is the only
  host-side change — Go, Node, `git`, `kubectl`, and everything
  else ship inside the image.

## Quick start

```sh
make uat-up           # build + bring the stack up
                      # -> http://localhost:8080

make uat-logs         # tail the Cooker container stdout
make uat-shell        # shell inside the Cooker container
                      # (kubectl, git, docker-cli all present)

make uat-down         # stop everything and wipe volumes
make uat-reset        # down + up
```

`uat-up` is idempotent. It creates `.env.uat` on first run with
a fresh `COOKER_SECRET_KEY`; `uat-down` wipes that file so every
fresh start has a new key.

What `uat-up` brings up (compose services):

| Service            | Purpose                                              |
|--------------------|------------------------------------------------------|
| `cooker`           | the app; mounts docker.sock + kubeconfig (ro)        |
| `postgres`         | persistent store                                     |
| `registry`         | CNCF Distribution; receives pushes, feeds k3s        |
| `k3s`              | single-node Kubernetes the Apps deploy into          |
| `kubeconfig-fixer` | rewrites `127.0.0.1` → `k3s` in the emitted config   |

## Test scenarios

### Scenario 1 — Happy path, Kubernetes target

1. Browse http://localhost:8080 → **Apps → New App**. Fill:
   - Name: `demo`
   - GitHub repo: any public repo with a root `Dockerfile`
     (e.g. `nginxinc/docker-nginx-unprivileged` branch `main`)
   - Branch: `main`
   - Deploy target: Kubernetes
   - Namespace: `default`
2. **Create**, then open the app.
3. **Deploy**. The log panel should stream:
   - `[clone] github.com/... @ main`
   - git clone progress
   - `[plan] detected kind=dockerfile`
   - docker build output
   - docker push output
   - `deployment.apps/demo created`
   - `service/demo created`
   - `[final] status=success`
4. Verify the workload from inside the container (kubectl is
   bundled in the image):

   ```sh
   make uat-shell
   kubectl get deploy,svc demo -n default
   ```

### Scenario 2 — Fails gracefully with missing CLI

1. Edit `docker-compose.uat.yml` → set `COOKER_BUILDER: noop` on
   the `cooker` service. `make uat-reset`.
2. Deploy an App. Expect the build stage to succeed (noop), the
   push stage to succeed (noop), and the deploy to fail only if
   the manifest references a non-existent image.
3. The failure surfaces in the log stream with a clear error —
   not a hung request.

### Scenario 3 — GitHub webhook (auto-deploy)

No public URL required — we forge the HMAC locally.

1. Set a webhook secret on an existing App:

   ```sh
   curl -X PUT http://localhost:8080/api/v1/apps/<ID>/webhook \
        -H 'Content-Type: application/json' \
        -d '{"secret":"hunter2"}'
   ```

2. Toggle `autoDeploy=true` via PUT on the App.
3. Simulate a push from GitHub:

   ```sh
   BODY='{"ref":"refs/heads/main","after":"abc","repository":{"full_name":"<owner/name>"}}'
   SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac 'hunter2' | awk '{print $2}')"
   curl -X POST http://localhost:8080/webhooks/github \
        -H 'X-GitHub-Event: push' \
        -H "X-Hub-Signature-256: $SIG" \
        -H 'Content-Type: application/json' \
        -d "$BODY"
   ```

4. Expect `202 Accepted` with `"status": "deploy queued"`.
5. A bad signature must return `401 Unauthorized`.

### Scenario 4 — Secrets

1. Create an Environment via the Environments page.
2. Seal a secret:

   ```sh
   curl -X PUT http://localhost:8080/api/v1/environments/<ENV_ID>/secrets/DB_PASSWORD \
        -H 'Content-Type: application/json' \
        -d '{"value":"supersecret"}'
   ```

3. Reveal it (admin-only endpoint; default dev user is admin):

   ```sh
   curl http://localhost:8080/api/v1/environments/<ENV_ID>/secrets/DB_PASSWORD
   # {"key":"DB_PASSWORD","value":"supersecret"}
   ```

4. List environments — the raw value never appears:

   ```sh
   curl http://localhost:8080/api/v1/environments | jq '.[].secretKeys'
   # [["DB_PASSWORD"]]
   ```

### Scenario 5 — Managed Hosts

`/hosts` isn't a menu item yet (frontend TBD), but the API
works:

```sh
curl -X POST http://localhost:8080/api/v1/hosts \
     -H 'Content-Type: application/json' \
     -d '{"name":"prod-docker","kind":"docker","reachability":"direct","dockerEndpoint":"tcp://10.0.0.3:2375"}'
```

List and delete via the analogous routes.

### Scenario 5b — SSH remote hosts (Dokploy / Coolify model)

Deploy a Cooker-built image to a GCP Compute Engine VM, a
DigitalOcean Droplet, or any plain Linux host with `docker`
installed — over SSH, no agent on the box, no Kubernetes, no
cloud APIs.

**Provision a sandbox host:**

1. Spin a fresh VM (e2-micro on GCP, the cheapest Droplet on DO,
   etc.). Install docker via the distro package manager.
2. Create a deploy user with `sudo`-less docker access:
   ```sh
   sudo useradd -m -G docker deploy
   sudo mkdir -p /home/deploy/.ssh
   sudo cp your-pub-key.pem /home/deploy/.ssh/authorized_keys
   sudo chown -R deploy:deploy /home/deploy/.ssh
   ```
3. Confirm: `ssh deploy@<vm-ip> docker version` succeeds from your
   workstation.

**Register the host in Cooker:**

UI path — Hosts page → "Add host" → kind = "SSH remote
(Dokploy / Coolify model)". Paste the PEM private key in the
textarea. Leave **Strict host-key check** enabled (the production
default). Save.

API path:

```sh
curl -X POST http://localhost:8080/api/v1/hosts \
     -H 'Content-Type: application/json' \
     -d '{
       "name": "sandbox-vm",
       "kind": "ssh-docker",
       "sshEndpoint": "1.2.3.4",
       "sshUser": "deploy",
       "sshPort": 22,
       "sshStrictHostKey": true,
       "sshPrivateKeyPem": "-----BEGIN OPENSSH PRIVATE KEY-----\n…"
     }'
```

The PEM body is stored encrypted via the secrets manager and is
**never** returned by any GET. Subsequent responses surface only
`hasSSHPrivateKey: true`.

**Point an App at the SSH host:**

```sh
curl -X PUT http://localhost:8080/api/v1/apps/<app-id> \
     -H 'Content-Type: application/json' \
     -d '{
       "name": "demo",
       "githubRepo": "you/demo",
       "branch": "main",
       "deployTarget": { "kind": "ssh", "hostId": "<host-uuid>" }
     }'
```

`DeployTargetSSH = "ssh"` is the canonical selector value for the
adapter. Use this as the `deployTarget.kind` on the App.

**Deploy:**

Click **Deploy** on the App page (or `POST /apps/:id/deploy`). The
run-log stream will show:

```
[ssh] docker pull nginx:alpine
[ssh] docker stop cooker-<app-id> (best-effort)
[ssh] docker rm cooker-<app-id> (best-effort)
[ssh] docker run -d --restart=always --name 'cooker-<app-id>' -p '80:80' 'nginx:alpine'
[ssh] pinned host key for <host-id>: ssh-ed25519 …
```

That last line — the **TOFU pin** — appears only on the first
successful connect when `sshStrictHostKey=false`. Subsequent
connects refuse if the server's key has changed.

**Verify the container is serving:**

```sh
curl http://<vm-ip>:80/   # the nginx welcome page
```

**Production note:** boot will fail if any registered SSH host has
`sshStrictHostKey=false` and `COOKER_ENV=production`. The check
runs after the store is open but before serving traffic; the
error message names the offending hosts so the operator can fix
them via PUT and restart.

### Scenario 6 — Registry round-trip

After a successful Scenario 1, confirm the image landed in the
bundled registry:

```sh
curl -s http://localhost:5001/v2/_catalog | jq
# {"repositories":["cooker/demo"]}

curl -s http://localhost:5001/v2/cooker/demo/tags/list | jq
```

## What works right now

| Area | State |
|------|-------|
| App CRUD | Real — Postgres |
| Deploy button (Clone→Build→Push→Deploy) | Real — uses `git`, `docker`, `kubectl` |
| Live build log over WebSocket | Real |
| Stage-log replay / `?since=` reconnect | Real — in-memory backend (single-replica); see [Stage-log replay config](#stage-log-replay-config) |
| GitHub webhook HMAC | Real — SHA-256 constant-time compare |
| Secret seal/reveal (admin-only) | Real — AES-GCM |
| RBAC: approver vs operator vs viewer | Real |
| Build-plan detection (Dockerfile/compose/buildpack) | Real |
| Environments, Pipelines, Runs | Real |
| Docker/K8s/Registry handlers | **Stubbed** — return empty lists |
| Networks / Volumes | **Stubbed** — routes exist, no Docker client yet |
| Cloud Run deploy target | **Stubbed** — returns `ErrUnavailable` |
| BuildKit gRPC, Crane push, client-go deploy | **Stubbed** — rely on CLI fallbacks for UAT |
| Tailscale tsnet transport | **Build-tagged** — needs `-tags tsnet` |
| GitOps commit (go-git) | **Stubbed** — Noop gives a deterministic fake SHA |

<a id="stage-log-replay-config"></a>
## Stage-log replay config

A WebSocket subscriber on `/ws/runs/:runId/stages/:stageId/logs` now
receives the **backlog so far** on connect, then live frames, and can
**resume after a known line** by passing `?since=<seq>` (the `seq` of the
last frame it saw). Each frame is a JSON envelope
`{"runId","stageId","seq","ts","line"}`; a dropped slow client gets one
`{"control":"stream-truncated"}` frame before the socket closes.

The backing store is selected at boot:

| Env var | Default | Meaning |
|---|---|---|
| `COOKER_LOGSTORE_BACKEND` | `memory` | Log-history backend. `memory` is the only implemented value; an unknown value logs a warning and falls back to `memory`. |
| `COOKER_LOGSTORE_MAX_BYTES` | `1048576` (1 MiB) | Per-stage retained-line byte cap. Oldest lines are dropped (ring buffer) once a stage exceeds it. |
| `COOKER_LOGSTORE_MAX_STREAMS` | `256` | Max concurrently retained stage streams. The least-recently-appended whole stream is evicted past this. |

## Build layer cache

`COOKER_BUILD_CACHE_REPO` (unset by default) stamps a registry
layer-cache ref onto the build stages of app-deploy synthesized
pipelines — Kaniko gets `--cache=true --cache-repo=`, Buildah gets
`--layers --cache-from/--cache-to`, BuildKit gets registry cache
import/export. Hand-built pipelines configure cache per build stage in
the editor instead ("Layer cache" section). The UAT compose's `noop`
builder ignores it, so this is only observable with a real builder.
See [`docs/build-cache.md`](../build-cache.md) for credentials and
per-builder semantics.

**Single-replica only.** The `memory` backend lives in one process, so
replay only covers stages handled by *this* replica — exactly the same
constraint as the in-memory WS hub and rate limiter. Durable / multi-replica
`postgres` and `redis` backends are described as future work in
`docs/proposals/execution-observability-redesign-2026.md` (Part A Phase 3)
and are not implemented yet. The default UAT compose is single-replica, so
no configuration is required.

## Pipeline power knobs (M2)

- **Per-pipeline run deadline** — the editor's "Run deadline" field (or
  `Pipeline.runDeadline` via API; Go duration, clamped [10s, 24h])
  overrides `COOKER_RUN_DEADLINE` for that pipeline's runs. Applies to
  both the inline spawn path and jobqueue workers.
- **Per-stage retry policy** — build/push/deploy stage panels expose
  `retry {maxAttempts, initialMs, maxMs, exponential}`; the legacy
  integer `retries` is still honoured when no structured policy is set.
- **Edge conditions** — click an edge in the editor to cycle
  success → failure → always. "failure" trajectories run when their
  upstream fails (e.g. notify-on-failure); "always" runs on any
  terminal upstream; stages whose conditions resolve to "don't run"
  finish as `skipped`. **Behaviour change:** with conditions enabled
  (default), one failed stage no longer aborts unrelated parallel
  branches mid-run — they complete and the run is still marked failed
  at the end. Set `COOKER_EDGE_CONDITIONS_ENABLED=false` to restore
  the legacy abort-on-first-failure behaviour.

## AI failure triage + analytics (M4)

- **AI triage** is off by default. `COOKER_AI_TRIAGE_ENABLED=true` +
  `ANTHROPIC_API_KEY` (and optionally `COOKER_AI_TRIAGE_MODEL`,
  default `claude-fable-5`) enable the "Why did this fail?" button on
  failed stages. **Data egress:** the failed stage's sanitized config
  summary, error and last 32 KiB of logs are sent to the Anthropic
  API on each click — never automatically. Env values and secret
  refs are stripped from the prompt; the API key never reaches the
  browser. Responses are advisory text only.
- **Insights page** (`/analytics`, pro mode) computes per-stage
  p50/p95/avg durations and success rates from the last 30 runs —
  no extra configuration.

## Audit trail + secrets probe (M5)

- **Queryable audit trail** — `COOKER_AUDIT_DESTINATION` now accepts a
  comma list of sinks; add `db` to write each mutating-API event into
  the `audit_events` table and browse/filter it at `/admin/audit`
  (admin + MFA; `GET /api/v1/admin/audit`).

  | Env var | Default | Meaning |
  |---|---|---|
  | `COOKER_AUDIT_ENABLED` | `true` in production, else `false` | Master switch for the audit middleware. |
  | `COOKER_AUDIT_DESTINATION` | `stdout` | Comma list of `stdout`, `file`, `db` (e.g. `db,stdout`). |
  | `COOKER_AUDIT_DB_RETENTION` | `2160h` (90 days) | Daily sweep deletes `audit_events` rows older than this. `0` disables the sweep. |

  **Trade-offs**: the `db` sink is queryable, durable, and
  retention-managed, but couples the audit trail to Postgres
  availability — the writer is async drop-on-full (like the file
  sink), so a Postgres outage *loses* events rather than blocking
  requests. The `file` sink is append-only and SIEM-shippable with no
  query path. Running `db,stdout` (or `db,file`) gives you the
  queryable viewer plus a loss-resistant stream. With no
  `DATABASE_URL` the db sink falls back to a non-durable in-memory
  ring (~10k events) and warns at boot; production refuses to start.
- **Secrets connectivity test** — Settings → Secrets tab (or
  `POST /api/v1/settings/secrets/test`, admin + MFA) probes the
  configured secrets backend with one authenticated `List` call and
  reports reachability + latency. Key names/values are never returned.
  In default UAT (database backend) expect `ok` with ~0 ms latency.

## Cloud inventory & cost panel (OR-2)

- **Off by default.** With no provider enabled, the **Cloud** page
  (sidebar, pro mode) shows a friendly "No cloud accounts configured"
  empty state and `GET /api/v1/cloud/inventory` returns
  `200 {"enabled":false}`. Nothing is queried and no SDK client is
  built — UAT smoke tests pass without any cloud credentials.
- **Read-only.** When enabled, Cooker lists compute instances, managed
  Kubernetes clusters, and container registries, plus month-to-date
  spend grouped by service. It only ever calls list/describe/cost APIs;
  there is no mutation path.

  | Env var | Default | Meaning |
  |---|---|---|
  | `COOKER_CLOUD_AWS_ENABLED` | `false` | Enable the AWS provider. |
  | `COOKER_CLOUD_AWS_REGION` | — | Required when AWS enabled (e.g. `ap-southeast-1`). |
  | `COOKER_CLOUD_AWS_ACCESS_KEY_ID` / `_SECRET_ACCESS_KEY` | — | Optional static creds; omit to use the AWS chain (IRSA / instance profile / env / shared config). |
  | `COOKER_CLOUD_GCP_ENABLED` | `false` | Enable the GCP provider. |
  | `COOKER_CLOUD_GCP_PROJECT_ID` | — | Required when GCP enabled. |
  | `COOKER_CLOUD_GCP_CREDENTIALS_JSON` | — | Optional SA-key JSON; omit to use ADC / Workload Identity (`GOOGLE_APPLICATION_CREDENTIALS`). |
  | `COOKER_CLOUD_CACHE_TTL` | `5m` | How often the cloud APIs are re-queried. AWS Cost Explorer bills per request. |

- **Partial failure is isolated.** If one provider's credentials are
  wrong, its tile shows a per-provider error banner and the other
  provider still renders — the request does not 500.
- **Refresh** (button, or `POST /api/v1/cloud/refresh`, operator role +
  rate-limited) busts the server cache and re-fetches.
- **GCP cost shows 0.00.** GCP month-to-date spend lives in the BigQuery
  billing export, not the Cloud Billing v1 API, so the GCP cost figure
  is a labelled zero until that export is wired — **this is expected**,
  don't file a bug. GCP *resources* are real.
- **Least privilege** — grant a read-only IAM identity. See
  [SECURITY.md → Cloud inventory credentials](../../SECURITY.md#cloud-inventory-credentials-read-only-opt-in).

## What's scaffolded (don't file bugs about these)

These are intentional placeholders documented as such. They'll
return `ErrUnavailable` or `"status":"pending"` and that's the
expected UAT behaviour:

- BuildKit gRPC client (use `COOKER_BUILDER=docker` instead)
- go-containerregistry pusher (use `COOKER_PUSHER=docker`)
- client-go deployer (use `COOKER_DEPLOYER=kubectl`)
- Test/Custom stage runner defaults to `noop` (logs the intended
  command, reports success, runs nothing). Set `COOKER_STAGE_RUNNER=docker`
  (or `kube`) to actually run Test/Custom stages in a container; a
  Test/Custom stage with no `image` fails loudly either way.
- Cloud Run, ECS, Fly, Render deploy targets
- go-git writer for the GitOpsCommit node
- Real Docker network/volume handlers
- Tailscale tsnet real transport in default builds

## Filing bugs

Open issues at https://github.com/santapong/cooker/issues with:

- Title: short, imperative (e.g. "Deploy button hangs when Dockerfile missing")
- Body:
  - **Scenario** (which test above, or custom)
  - **Expected** vs **actual** behaviour
  - Full log from the Apps → Detail page (copy the black box)
  - Backend log lines from `make uat-logs`
  - Output of `docker compose -f docker-compose.uat.yml ps` so we
    see which services are unhealthy
- Label: `uat` if you can (helps triage)

Once an issue is filed, Claude Code can read it, reproduce
locally if possible, push a fix to the
`claude/cooker-project-review-CheZS` branch, and comment on the
issue with the commit SHA.

## Known limitations (UAT compose)

- **Host daemon insecure-registries edit is required** (see
  Prerequisites). Cooker pushes via the host's docker daemon
  over the mounted socket, and the daemon refuses plaintext by
  default. Follow-up if this is a blocker: split
  `COOKER_REGISTRY_PUSH` and `COOKER_REGISTRY` in the Go code so
  pushes can go to `localhost:5001/cooker` while manifests
  reference `registry:5000/cooker`.
- The Cooker container runs as **non-root** UID 65532. `make uat-up`
  resolves the host's docker group GID and writes it as `DOCKER_GID`
  in `.env.uat` so the container can access the bind-mounted
  `/var/run/docker.sock`. If auto-detection picks the wrong GID
  for your host (rare; fallback is 999), edit `DOCKER_GID` in
  `.env.uat` and `make uat-reset`.
- k3s runs **privileged** with host cgroups — UAT-only.
- Single-node k3s with traefik + servicelb disabled. No Ingress,
  no LoadBalancer. Services are `ClusterIP`; reach deployed apps
  via `make uat-shell && kubectl port-forward svc/<name> 8000:80`.
- Registry has no auth and no TLS.
- **Authentication is off by default** in UAT — every request is
  treated as the built-in admin `dev-user`. To exercise the real
  OIDC flow, see "Enabling OIDC sign-in for UAT" below. Do not
  expose port 8080 to the internet while auth is off.
- **WebSocket reconnect is not automatic.** If the Cooker
  container restarts mid-deploy, refresh the App detail page
  and trigger a new deploy.
- **The synthesised Kubernetes manifest is minimal** (Deployment
  + Service on port 80). Custom workloads need explicit
  manifests via a Pipeline (not yet wired to an App's Deploy
  button).
- **Runs from `/apps/:id/deploy` are stored under a synthetic
  pipeline ID `app-<appId>`** which doesn't appear in the
  Pipelines list. Intentional for the current phase; a proper
  "App runs" view is a follow-up.

## `COOKER_ENV`

The compose stack sets `COOKER_ENV=uat`, which keeps the lenient
defaults (localhost CORS allowlist, no fatal startup checks) so
testers don't need to configure secrets and origins explicitly.

Production (Helm chart) sets `COOKER_ENV=production`, which:
- Defaults `COOKER_ALLOWED_ORIGINS` to **deny-all** (must be set explicitly).
- Adds startup validation that fails fast on missing `COOKER_SECRET_KEY`
  (added in PR B).

If you want UAT to behave like production for a one-off test, set
`COOKER_ENV=production` in `.env.uat` and provide
`COOKER_ALLOWED_ORIGINS=http://localhost:8080`.

## Enabling OIDC sign-in for UAT

UAT defaults to auth-off because most testers don't want to wire
an IdP just to click Deploy. When you do need to exercise the
real sign-in flow (e.g. before promoting a build to staging), do
this:

1. **Copy the env template** if you haven't already:

   ```sh
   cp .env.uat.example .env.uat
   ```

   `make uat-up` does this automatically on first run and appends
   a fresh `COOKER_SECRET_KEY`.

2. **Pick a provider section** in `.env.uat` (Google or KeepSave),
   uncomment it, fill in the values, save.

3. **Rebuild the stack** so Vite re-bakes the `VITE_OIDC_*` values
   into the JS bundle:

   ```sh
   make uat-reset
   ```

4. Visit http://localhost:8080 — you'll be redirected to the IdP
   login page; after sign-in you land back at `/callback` and
   then the Apps page.

### Provider: Google

Easiest for solo testing. Google issues real OIDC JWTs and the
backend validates them out of the box.

1. https://console.cloud.google.com/apis/credentials → **Create
   Credentials** → **OAuth client ID** → type **Web application**.
2. **Authorized redirect URI**: `http://localhost:8080/callback`.
3. Copy the **Client ID** (the secret is not used — the browser
   uses PKCE).
4. In `.env.uat`, uncomment the **Provider preset: Google**
   block and paste the Client ID into both
   `COOKER_OIDC_CLIENT_ID` and `VITE_OIDC_CLIENT_ID`.

> Caveat: Google does not emit a `groups` claim by default, so
> all signed-in users fall back to the **viewer** role per
> `backend/internal/auth/rbac.go:95`. To exercise admin/operator
> flows, prefer KeepSave (or stand up Keycloak — out of scope
> for UAT).

### Provider: KeepSave

Use this when you want full RBAC (admin / operator / approver /
viewer roles) backed by your own IdP.

1. Register Cooker as an OIDC client in KeepSave with redirect
   URI `http://localhost:8080/callback` and PKCE enabled.
2. Configure KeepSave to emit a `groups` claim containing one of:
   `cooker-admins`, `cooker-operators`, `cooker-approvers`, or
   `cooker-viewers` (mapping in `backend/internal/auth/rbac.go:77`).
3. In `.env.uat`, uncomment the **Provider preset: KeepSave**
   block and set:
   - `COOKER_OIDC_ISSUER_URL` / `VITE_OIDC_AUTHORITY` to your
     KeepSave base URL (it must serve
     `/.well-known/openid-configuration`).
   - `COOKER_OIDC_CLIENT_ID` / `VITE_OIDC_CLIENT_ID` to the
     client ID you registered.

### Switching back to auth-off

```sh
make uat-down
# edit .env.uat: comment out COOKER_OIDC_ENABLED / VITE_OIDC_ENABLED
make uat-up
```

Or `rm .env.uat && make uat-up` for a clean slate.

## Post-UAT follow-ups

- **Helm chart** (`deploy/helm/cooker`) — expose the same
  `COOKER_REGISTRY`/`BUILDER`/`PUSHER`/`DEPLOYER`/`SECRET_KEY`
  env vars, add an Ingress template, and document an
  in-cluster registry sidecar option.
- **Publish the Docker image to GHCR** via
  `.github/workflows/ci.yml` (currently builds but does not
  push).
- **Real backends**: BuildKit gRPC, go-containerregistry
  (crane), client-go dynamic deployer, go-git writer, Cloud Run
  / ECS / Fly / Render adapters.

---

## Alternative: run from source for development

If you're hacking on Cooker and want reload-on-save, skip
`make uat-up` and run the backend + Vite dev server directly.
You'll need Go 1.24+, Node 20+, and a local `git` / `docker` /
`kubectl` on PATH.

```sh
export DATABASE_URL=""                           # in-memory
export COOKER_REGISTRY="localhost:5000/cooker"
export COOKER_BUILDER="docker"
export COOKER_PUSHER="docker"
export COOKER_DEPLOYER="kubectl"
export COOKER_STAGE_RUNNER="docker"             # Test/Custom stages; "noop" to skip

export KUBECONFIG="$HOME/.kube/config"
export COOKER_SECRET_KEY="$(head -c 32 /dev/urandom | base64)"
export COOKER_OIDC_ENABLED=false
export COOKER_ALLOWED_ORIGINS="http://localhost:5173"
```

Two terminals:

```sh
# Terminal 1
cd backend && go run ./cmd/cooker     # :8080
```

```sh
# Terminal 2
cd frontend && npm install && npm run dev   # :5173
```

Visit http://localhost:5173.
