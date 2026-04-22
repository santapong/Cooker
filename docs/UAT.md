# Cooker — UAT Runbook

One command to start, a reachable URL, a working build+deploy
target, and clean teardown. Intended for testers exercising the
Apps "Deploy" button and filing bugs.

> **Status:** UAT-ready, not production-ready. Read the "What
> works" and "What's scaffolded" sections before you start so you
> know what to hit and what to skip.

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

## What's scaffolded (don't file bugs about these)

These are intentional placeholders documented as such. They'll
return `ErrUnavailable` or `"status":"pending"` and that's the
expected UAT behaviour:

- BuildKit gRPC client (use `COOKER_BUILDER=docker` instead)
- go-containerregistry pusher (use `COOKER_PUSHER=docker`)
- client-go deployer (use `COOKER_DEPLOYER=kubectl`)
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
- The Cooker container runs as **root** — acceptable for UAT,
  not for production.
- k3s runs **privileged** with host cgroups — UAT-only.
- Single-node k3s with traefik + servicelb disabled. No Ingress,
  no LoadBalancer. Services are `ClusterIP`; reach deployed apps
  via `make uat-shell && kubectl port-forward svc/<name> 8000:80`.
- Registry has no auth and no TLS.
- **No authentication** on the Cooker API in UAT mode. Every
  request is treated as the built-in admin `dev-user`. Do not
  expose port 8080 to the internet.
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
