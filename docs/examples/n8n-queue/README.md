# Local n8n queue example

This is a disposable, loopback-only Compose example for reviewing Cooker's native deployment graph. It has PostgreSQL, Redis, one n8n main process and one worker with five job slots. It is not a production profile.

Set `POSTGRES_PASSWORD` and `N8N_ENCRYPTION_KEY` privately before use. Main and worker must share the same encryption key. Set `N8N_IMAGE` to an explicitly reviewed version/digest; the fixture test uses n8n 2.28.6 only to check parsing, not to recommend that version or execute it. `POSTGRES_IMAGE`, `REDIS_IMAGE` and `N8N_PORT` can override the defaults. Never commit populated environment files.

From the repository root, inspect interpolation without printing secrets:

```sh
docker compose -p cooker-n8n-example-<unique-suffix> -f docs/examples/n8n-queue/compose.yaml config --quiet
```

Before starting anything, check current image suitability and host capacity, use fresh project-scoped volumes, and apply verified aggregate CPU/RAM/PID/time/disk limits. The example's memory limits alone are not a complete execution budget. This repository check does not start containers, contact a provider, configure OAuth or use the existing Pi installation.

`TestN8nQueueExampleComposeReviewAndDAG` exercises the actual Compose loader, runtime renderer and pipeline synthesizer. It requires four image-only Deploy stages and five startup dependencies. Main and worker both connect to PostgreSQL and Redis; the extra main-to-worker startup dependency serializes a fresh installation's migrations. The worker is one container with concurrency five. Editing a diagram does not run or scale this stack.

The configuration uses local HTTP and disables secure cookies and Code-node runners. Adapt HTTPS, access controls, runners, persistence, backups and operational limits before any real installation. A parsing/DAG pass does not prove queue delivery, recovery or deployment readiness. A full runtime check must independently observe a waiting job while the disposable worker is stopped, completion after restart, matching database/log evidence and owned cleanup.

Remove only the unique disposable project and its test volumes when finished:

```sh
docker compose -p cooker-n8n-example-<unique-suffix> -f docs/examples/n8n-queue/compose.yaml down --volumes
```
