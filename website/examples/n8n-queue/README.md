# n8n queue mode — tested local example

Four services: n8n main/web, one n8n worker, Redis, and PostgreSQL. Main and worker use the same pinned n8n image, database, Redis instance, and encryption key. The worker command is `worker --concurrency=5` (five concurrent jobs, not five workers).

Verified 28 September 2026 with n8n 2.28.6, Docker 29.8.0 and Compose v2.39.4. Image digests in `env.example` identify the cached versions actually tested; they are not a claim of the newest recommended production release.

## Try locally

Download `compose.yaml` and `env.example` into a new folder. Copy `env.example` to `.env`, set two different random values for the password and encryption key, and protect the file from other users. Choose a free loopback port if 19568 is occupied.

```sh
chmod 600 .env
docker compose -p my-n8n-queue --env-file .env config --quiet
docker compose -p my-n8n-queue --env-file .env up -d --wait
```

Open http://127.0.0.1:19568 and create the owner account for this new instance. Redis, PostgreSQL and the worker have no published ports. The named volumes preserve data when containers are recreated. All services share a project-scoped bridge network.

Main waits for the database and Redis to become healthy. Worker also waits for main readiness, so a fresh database completes migrations first. Worker readiness is enabled with `QUEUE_HEALTH_CHECK_ACTIVE=true`.

To stop the example without deleting stored data:

```sh
docker compose -p my-n8n-queue --env-file .env down
```

This is a local HTTP test profile. It sets secure cookies off only for loopback HTTP, uses debug logs, omits Code-node runners, and is not a production baseline. A real installation needs HTTPS, secret handling, backups, resource sizing and, when using Code nodes, the appropriate task-runner setup. The test has no binary data; do not assume local filesystem binary storage works across queue workers.

## What was actually tested

A temporary Webhook → Respond to Webhook workflow was activated in an isolated local instance. With the only worker stopped, the request remained pending and Redis held job #1. Starting the worker completed the response with the expected marker. PostgreSQL recorded execution #1 as `success`, and worker logs corroborated execution. The disposable stack was stopped and removed after verification; it is not a running service behind the design preview.

Separately, Cooker's actual Compose inspection, native runtime rendering and DAG synthesis passed a focused test using this file: four image-only services, four deployment stages, five dependency edges, preserved healthy conditions, worker command, shared key and loopback port. No Build or Push stages are needed because the images already exist. This was not a deployment through Cooker's GitHub review UI or a production acceptance test.

Cooker's reviewed App path currently supports one replica per named service. n8n itself supports multiple workers; automatic scaling is not demonstrated by this example. Dedicated webhook processors are optional.

## Sources

- [n8n queue mode](https://docs.n8n.io/deploy/host-n8n/configure-n8n/scaling/enable-queue-mode)
- [Cooker source](https://github.com/santapong/Cooker)

Local evidence: `/mnt/data/cooker-n8n-evidence-2026-09-28/`. The downloaded package contains no test passwords, encryption keys, login cookies, or private database files.
