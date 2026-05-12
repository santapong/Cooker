# Self-hosting tips

Operational guidance for running Cooker yourself. The [Helm install](../getting-started/helm-install.md) page covers the standard install; this page is the long tail — TLS, reverse proxies, scaling, resource sizing.

## TLS termination

Cooker does not terminate TLS. Use one of:

| Pattern | Effort | When |
|---|---|---|
| cert-manager + Let's Encrypt + nginx ingress | 30 min | Standard. Documented in [Helm install](../getting-started/helm-install.md#tls-at-ingress). |
| Cloud LB (ALB / GLB / Azure) with managed cert | 20 min | Cloud-native installs. |
| Reverse-proxy in front of the cooker container | 1 hour | Compose / single-VM. |

### Reverse proxy in front of compose

Caddy is the simplest pattern. `Caddyfile`:

```caddyfile
cooker.example.com {
  reverse_proxy localhost:8080
}
```

Then `caddy run --config Caddyfile`. Caddy auto-provisions a Let's Encrypt cert on first request.

WebSocket upgrades work through Caddy out of the box. For nginx you may need:

```nginx
location / {
  proxy_pass http://localhost:8080;
  proxy_http_version 1.1;
  proxy_set_header Upgrade $http_upgrade;
  proxy_set_header Connection "upgrade";
  proxy_set_header Host $host;
  proxy_read_timeout 86400;
}
```

The `proxy_read_timeout 86400` matters for long-lived WebSocket connections during long builds.

## CORS

`COOKER_ALLOWED_ORIGINS` is a CSV of exact origins. Production rejects `*`. Set it to the exact `https://cooker.example.com` value the browser will see.

For local cross-port dev (UI on `:5173`, API on `:8080`), the defaults `http://localhost:5173,http://localhost:3000` apply when `COOKER_ENV=dev|uat`.

Cooker explicitly does **not** emit `Allow-Credentials: true` — the API authenticates by `Authorization: Bearer <jwt>`, not by cookies. Don't add an ingress annotation that flips this on; it gains nothing and breaks wildcard origin reflection.

## Multi-replica

> **Important.** Two pieces of state are per-process: the rate limiter and the WebSocket ticket store. Running ≥ 2 replicas WITHOUT one of the two fixes below will fail `Config.Validate()` in production.

Two paths:

### 1. Sticky sessions (simpler)

Set `COOKER_STICKY_SESSIONS=true` and configure ingress affinity. The full annotation snippets for NGINX / ALB / Traefik / HAProxy / Envoy are in [`docs/MULTI_REPLICA.md`](../../MULTI_REPLICA.md).

Symptoms if you skip the annotations:

- Random `401 Unauthorized` on WebSocket upgrades (ticket issued on replica A, redeemed on replica B which doesn't know about it).
- Inconsistent rate-limit enforcement (the limiter only sees half your traffic per replica).

### 2. Redis-backed state (proper HA)

Switch all three to redis:

```bash
COOKER_RATE_LIMIT_BACKEND=redis
COOKER_WS_TICKET_BACKEND=redis
COOKER_WS_HUB_BACKEND=redis
REDIS_URL=redis://cooker-redis:6379
```

Rate limiting uses GCRA via `go-redis/redis_rate/v10`; WS tickets use atomic `GETDEL` so a single ticket can never be redeemed twice across replicas; the WS hub uses Redis pub/sub so any replica can fan out to its connected clients.

## Resource requests

Cooker is a Go binary serving HTTP. Sizing scales with WebSocket connection count and concurrent run count, not with the number of pipelines.

Sensible starting points:

| Replicas | CPU request | Memory request |
|---|---|---|
| 1 (small team) | 100m | 256Mi |
| 2 (HA, small team) | 100m each | 256Mi each |
| 3+ (large team) | 200m each | 512Mi each |

Build Jobs (Kaniko / Buildah) have separate resource requirements — they run in their own pods. Size those based on your largest expected build context (typically the source repo size + compile output).

> **Known limitation.** Kaniko / Buildah Job specs do NOT yet expose `nodeSelector` or `tolerations`. You can't pin builds to a CPU-only pool away from GPU nodes via a Cooker setting today. Tracked under W11 ML persona gaps; roadmap items `D7` / `D8`.

## Postgres sizing

The DB is light. Pipelines and runs are JSONB documents; the biggest growth area is `pipeline_runs.stage_runs.logs`.

| Install | Storage start | Growth |
|---|---|---|
| Single team / 10 apps | 1 GiB | ~50 MiB / month |
| Mid-size / 50 apps | 10 GiB | ~500 MiB / month |
| Enterprise / 250 apps | 50 GiB | ~2 GiB / month |

If logs are large (ML builds, etc.), `pipeline_runs.stage_runs` is the JSONB column to watch. A retention CronJob ships in the chart (`templates/cronjob-retention.yaml`) that prunes runs older than the configured threshold; see [Postgres](../operations/postgres.md#retention).

## Redis sizing

`50Mi` is plenty for the rate limiter and WS tickets. The WS hub doesn't store state — it's pub/sub fan-out.

## Backups

See [Postgres backup and restore](../operations/postgres.md#backup-and-restore). The minimum-viable backup is `pg_dump` on a CronJob; the right one is `pg_basebackup` + WAL archiving via WAL-G, Barman, or your managed-DB equivalent.

## Logs

Cooker logs JSON to stderr (`log/slog`). In K8s, ship via your existing log stack (Loki, ELK, Datadog, CloudWatch). The structured fields are documented in [Observability](../operations/observability.md#logs).

## Security headers

Add at the ingress / reverse proxy:

```text
Content-Security-Policy: default-src 'self'; connect-src 'self' wss:;
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Strict-Transport-Security: max-age=31536000; includeSubDomains
Referrer-Policy: strict-origin-when-cross-origin
```

The chart's middleware adds most of these for in-app responses already; the ingress layer covers the SPA's static files.

## Backup strategy summary

| What | Why | How |
|---|---|---|
| Postgres | Pipelines, runs, environments, apps, hosts, secret ciphertext | `pg_dump` daily; WAL archiving continuous. |
| `COOKER_SECRET_KEY` | Decrypts `database`-backend secrets. Lose this = lose every secret. | Store in your password manager / Vault / KMS. Multiple copies. |
| Helm values | Reproducible install | `git`. Always. |

## What you don't need to back up

- The Cooker binary / Docker image — pull from the registry.
- Migrations — embedded in the binary.
- Static frontend assets — also embedded.

## Cross-references

- **[Helm install](../getting-started/helm-install.md)** — TLS, OIDC, the production install.
- **[`docs/MULTI_REPLICA.md`](../../MULTI_REPLICA.md)** — ingress affinity snippets per controller.
- **[`docs/RUNBOOK.md`](../../RUNBOOK.md)** — symptom-driven incident response.
- **[Postgres](../operations/postgres.md)** — DB setup and backup.
