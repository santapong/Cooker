# Quickstart

Goal: have Cooker running, signed in as the dev admin, with a working build-and-deploy pipeline against a single-node k3s cluster, in **under five minutes**.

This uses the bundled UAT compose stack. For production install via Helm, see [Helm install](helm-install.md).

## Prerequisites

- **Docker 24+** with the daemon running. On Linux you must be able to run `docker` without `sudo`.
- **`make`**, **~10 GB free disk**, **~4 GB free RAM**.
- A one-time edit to `/etc/docker/daemon.json` to add the in-stack registry to Docker's insecure list. The UAT stack pushes via the host's docker daemon, and the daemon refuses plaintext pushes by default.

  ```json
  { "insecure-registries": ["registry:5000"] }
  ```

  Then restart Docker (`sudo systemctl restart docker` on Linux; Docker Desktop -> Restart on macOS / Windows). This is the only host-side change — `git`, `kubectl`, and everything else ship inside the image.

## Bring it up

```sh
git clone https://github.com/santapong/cooker.git
cd cooker
make uat-up
```

`make uat-up` is idempotent. On first run it creates `.env.uat` with a fresh `COOKER_SECRET_KEY`. `make uat-down` wipes that file so the next start gets a fresh key.

When the build finishes, open <http://localhost:8080>. You are signed in as the dev admin automatically; see [Configuration](configuration.md#dev-mode-auth-off) for why that's safe in UAT but never in production.

## What's running

| Service | Purpose |
|---|---|
| `cooker` | The Go binary serving the API and the React frontend on port 8080. |
| `postgres` | Persistent store for pipelines, runs, environments, apps, hosts. |
| `registry` | CNCF Distribution; receives pushes, feeds k3s. Reachable as `registry:5000` from inside the network. |
| `k3s` | Single-node Kubernetes. The default deploy target. |
| `kubeconfig-fixer` | Rewrites `127.0.0.1` to `k3s` in the kubeconfig emitted by k3s so the Cooker container can dial it. |

## Deploy your first App

This walks through the happy-path scenario from [`docs/UAT.md`](../../UAT.md#scenario-1--happy-path-kubernetes-target).

1. In the UI, go to **Apps -> New App** and fill:
   - **Name:** `demo`
   - **GitHub repo:** any public repo with a root `Dockerfile`. `nginxinc/docker-nginx-unprivileged` on `main` is a known-good test.
   - **Branch:** `main`
   - **Deploy target:** Kubernetes
   - **Namespace:** `default`
2. **Create**, then open the app.
3. Click **Deploy**. The log panel streams over WebSocket:

   ```text
   [clone]  github.com/... @ main
   [plan]   detected kind=dockerfile
   [build]  ...docker build output...
   [push]   ...docker push output...
   [deploy] deployment.apps/demo created
   [deploy] service/demo created
   [final]  status=success
   ```

4. Verify from inside the Cooker container:

   ```sh
   make uat-shell
   kubectl get deploy,svc demo -n default
   ```

You can confirm the image landed in the bundled registry:

```sh
curl -s http://localhost:5001/v2/_catalog | jq
curl -s http://localhost:5001/v2/cooker/demo/tags/list | jq
```

## Reach the deployed app

The UAT k3s has Traefik and `servicelb` disabled — there is no Ingress and no LoadBalancer. Services are `ClusterIP`. Port-forward to reach them:

```sh
make uat-shell
kubectl port-forward svc/demo 8000:80
# then on the host:
curl http://localhost:8000
```

> **Partial.** The synthesised Kubernetes manifest is a minimal Deployment + Service on port 80. Custom manifests need a Pipeline (which isn't yet wired to the App's Deploy button). Tracked in [`docs/UAT.md`](../../UAT.md#known-limitations-uat-compose).

## Tear it down

```sh
make uat-down       # stop everything and wipe volumes
make uat-reset      # down + up in one go
```

## Next steps

- **[Configuration](configuration.md)** — env vars, `COOKER_ENV` modes, defaults.
- **[Your first pipeline](../guides/first-pipeline.md)** — graph editor end to end, the hard way.
- **[GitHub webhooks](../guides/github-webhooks.md)** — turn this manual Deploy into auto-on-push.
- **[Concepts: Apps vs Pipelines vs Environments](../concepts/apps.md)** — understand the model before scaling up.

## What didn't work?

If `make uat-up` fails, check [Troubleshooting](../operations/troubleshooting.md) first — the top entry covers the three most common UAT issues. If the Deploy stream hangs, see [Builds stuck](../troubleshooting/builds-stuck.md).
