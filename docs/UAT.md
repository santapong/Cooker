# Cooker — UAT Runbook

This is the minimum path to exercise Cooker end-to-end on a single
development machine, plus the test scenarios and how to file bugs.

> **Status:** UAT-ready, not production-ready. Read the "What works"
> and "What's scaffolded" sections before you start testing so you
> know what to hit and what to skip.

## Prerequisites

On the UAT machine:

- Go 1.24+
- Node 20+
- `git` CLI on PATH
- `docker` CLI + a running Docker daemon
- `kubectl` CLI + a reachable cluster (kind, minikube, or remote)
- A container registry you can push to. For fully-local testing, run
  the CNCF Distribution registry:

  ```sh
  docker run -d -p 5000:5000 --name registry registry:2
  ```

  Then your registry is `localhost:5000`.

## Configuration

Cooker reads config from env vars. The defaults are safe but the
build/push/deploy backends are `noop` unless you set them.

```sh
# Store: empty DATABASE_URL uses in-memory (no persistence across restarts).
# Set to your Postgres DSN for real use.
export DATABASE_URL=""

# Image registry prefix used when an App doesn't override it.
export COOKER_REGISTRY="localhost:5000/cooker"

# Backends for UAT. All three need the matching CLIs on PATH.
export COOKER_BUILDER="docker"    # shells out to `docker build`
export COOKER_PUSHER="docker"     # `docker tag` + `docker push`
export COOKER_DEPLOYER="kubectl"  # `kubectl apply -f -`

# Kubernetes: path to kubeconfig (leave empty for in-cluster).
export KUBECONFIG="$HOME/.kube/config"

# Secrets-at-rest. 32 random bytes, base64. Generate once and keep it.
export COOKER_SECRET_KEY="$(head -c 32 /dev/urandom | base64)"

# Auth: leave OIDC disabled for UAT — a default admin "dev-user" is
# injected on every request so you can hit admin endpoints without a
# real IdP.
export COOKER_OIDC_ENABLED=false

# CORS: the frontend runs on Vite's default port during UAT.
export COOKER_ALLOWED_ORIGINS="http://localhost:5173"
```

## Run

Two terminals:

```sh
# Terminal 1 — backend
cd backend
go run ./cmd/cooker
# Cooker starts on :8080
```

```sh
# Terminal 2 — frontend dev server
cd frontend
npm install
npm run dev
# Vite reports http://localhost:5173
```

Visit http://localhost:5173. The default route is **Apps**.

## Test scenarios

### Scenario 1 — Happy path, Kubernetes target

1. **Apps → New App**. Fill:
   - Name: `demo`
   - GitHub repo: any public repo with a root `Dockerfile`
     (e.g. `nginxinc/docker-nginx-unprivileged` branch `main`)
   - Branch: `main`
   - Deploy target: Kubernetes
   - Namespace: `default`
2. Click **Create**, then open the app.
3. Click **Deploy**. The log panel should stream:
   - `[clone] github.com/... @ main`
   - git clone progress
   - `[plan] detected kind=dockerfile`
   - docker build output
   - docker push output
   - kubectl apply output
   - `[final] status=success`
4. Verify the workload:
   ```sh
   kubectl get deploy,svc demo -n default
   ```

### Scenario 2 — Fails gracefully with missing CLI

1. Unset one of the CLIs: `export COOKER_BUILDER=noop`, restart.
2. Deploy an App. Expect the build stage to succeed (noop), the push
   stage to succeed (noop), and the deploy to fail only if the
   manifest references a non-existent image.
3. The failure surfaces in the log stream with a clear error — not
   a hung request.

### Scenario 3 — GitHub webhook (auto-deploy)

1. Set a webhook secret: **Apps → your app → API (use curl for now)**:
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
4. List environments:
   ```sh
   curl http://localhost:8080/api/v1/environments | jq '.[].secretKeys'
   # [["DB_PASSWORD"]]  ← raw value never appears
   ```

### Scenario 5 — Managed Hosts

1. **Hosts** isn't a menu item yet (frontend TBD), but the API works:
   ```sh
   curl -X POST http://localhost:8080/api/v1/hosts \
        -H 'Content-Type: application/json' \
        -d '{"name":"prod-docker","kind":"docker","reachability":"direct","dockerEndpoint":"tcp://10.0.0.3:2375"}'
   ```
2. List and delete via the analogous routes.

## What works right now

| Area | State |
|------|-------|
| App CRUD | Real — Postgres or in-memory |
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
  - Backend log lines from the terminal running `go run ./cmd/cooker`
  - `COOKER_BUILDER`, `COOKER_PUSHER`, `COOKER_DEPLOYER` values
- Label: `uat` if you can (helps triage)

Once an issue is filed, Claude Code can read it, reproduce locally
if possible, push a fix to the `claude/cooker-project-review-CheZS`
branch, and comment on the issue with the commit SHA.

## Known limitations

- **No authentication** in UAT mode. Every request is treated as the
  built-in admin `dev-user`. Do not expose the port to the internet.
- **In-memory store loses data on restart** unless you set
  `DATABASE_URL`.
- **WebSocket reconnect is not automatic.** If the backend restarts
  mid-deploy, refresh the App detail page and trigger a new deploy.
- **The synthesised Kubernetes manifest is minimal** (Deployment +
  Service on port 80). Custom workloads need explicit manifests
  via a Pipeline (not yet wired to an App's Deploy button).
- **Runs from /apps/:id/deploy are stored under a synthetic
  pipeline ID `app-<appId>`** which doesn't appear in the Pipelines
  list. This is intentional for Phase 3; a proper "App runs" view
  ships with Phase 8.
