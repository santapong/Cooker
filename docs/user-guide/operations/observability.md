# Observability

Three signals: logs, metrics, traces. Plus health endpoints for orchestrator probing and audit logs for compliance.

## Health endpoints

| Endpoint | Returns | Use for |
|---|---|---|
| `GET /health` | 200 OK once the HTTP server is up. | Legacy. Equivalent to `/health/live`. |
| `GET /health/live` | 200 OK once the HTTP server is up. | Kubernetes `livenessProbe`. |
| `GET /health/ready` | 200 OK only if the store ping succeeded recently. | Kubernetes `readinessProbe`, load balancers. |

All three live OUTSIDE the OIDC middleware — orchestrators must be able to reach them without credentials.

`/health/ready` checks:

- The configured store's `Ping()` succeeded in the last 30 seconds.
- If Redis is configured, Redis `Ping()` succeeded.

A failing readiness probe pulls the pod out of the Service's endpoints, so traffic shifts to healthy replicas (or queues if there are none). The pod is NOT killed — that's the liveness probe's job, and Cooker's `/live` is always 200 once HTTP is up.

> **TODO: verify** the `/health/ready` cache window (30s used in `server.go` per the `readinessHandler` test file). <!-- TODO: verify -->

## Logs

Cooker logs JSON to stderr via Go's `log/slog`. Every line carries:

```json
{
  "time":  "2026-05-12T10:30:00Z",
  "level": "INFO",
  "msg":   "stage started",
  "pipeline": "p_abc",
  "run":      "r_xyz",
  "stage":    "build"
}
```

Field conventions:

- `pipeline=<id>`, `run=<id>`, `stage=<id>` for run-related logs.
- `app=<id>` for app-level events.
- `err` for the error value; never the stack trace (slog handles wrapping).
- `route=/api/v1/pipelines/:id` for request logs (template form, not concrete IDs).

The default level is `INFO`. To bump to `DEBUG`:

```bash
COOKER_LOG_LEVEL=debug
```

> **TODO: verify** that `COOKER_LOG_LEVEL` is actually wired. `main.go` calls `slog.SetDefault` with default options; level configuration may not be wired yet. <!-- TODO: verify -->

### Forwarding to your log stack

In K8s, your log shipper (Fluent Bit, Vector, Loki Promtail, Datadog Agent, etc.) picks up stderr from the pod automatically. Cooker doesn't write to files by default — the only file output is the audit log when `COOKER_AUDIT_DESTINATION=file`.

## Metrics

Opt-in via:

```bash
COOKER_METRICS_ENABLED=true
```

Exposes Prometheus exposition format on `/metrics` (same port as the API). Hit it directly to inspect:

```bash
curl https://cooker.example.com/metrics
```

### Series

The middleware emits two HTTP-level series:

| Metric | Type | Labels |
|---|---|---|
| `cooker_http_requests_total` | counter | `method`, `route`, `status` |
| `cooker_http_request_duration_seconds` | histogram | `method`, `route` |

`route` is the Gin matched template (e.g. `/api/v1/pipelines/:id`), NOT the concrete URL. This keeps cardinality bounded.

The audit middleware exposes:

| Metric | Type | Labels |
|---|---|---|
| `cooker_audit_events_total` | counter | — |
| `cooker_audit_events_dropped_total` | counter | (incremented when the async writer drops on backpressure) |

> **TODO: verify** the full list of cooker-specific metrics — there may be more (queue depth, in-flight runs, etc.) under `internal/observability/`. <!-- TODO: verify -->

### Scraping

```yaml
# Prometheus scrape config
scrape_configs:
  - job_name: cooker
    metrics_path: /metrics
    static_configs:
      - targets: ['cooker.cooker.svc.cluster.local:8080']
```

The chart's `Service` exposes the same `:8080` port for both API and metrics — no separate metrics port. Use the chart value `serviceMonitor.enabled=true` to render a Prometheus Operator `ServiceMonitor` automatically.

## Traces

Opt-in via:

```bash
COOKER_TRACING_ENABLED=true
COOKER_OTLP_ENDPOINT=otel-collector.observability.svc.cluster.local:4317
COOKER_OTLP_INSECURE=true       # in-cluster OTLP rarely uses TLS
COOKER_SERVICE_NAME=cooker
COOKER_SERVICE_VERSION=v0.1.0
```

The exporter is OTLP/gRPC. The integration uses `otelgin` for HTTP span creation; route templates are the span names (same as metrics).

Span attributes include:

- HTTP method, route template, status code, latency.
- `cooker.pipeline.id` and `cooker.run.id` for run-spawning routes.

Backends:

- **Jaeger** — point `COOKER_OTLP_ENDPOINT` at your Jaeger Collector's OTLP listener (default 4317).
- **Grafana Tempo** — same.
- **Honeycomb** — set `COOKER_OTLP_ENDPOINT=api.honeycomb.io:443`, `COOKER_OTLP_INSECURE=false`, and a `x-honeycomb-team: <key>` header (today only via the OTel collector — no direct env var for headers).
- **Datadog** — via the Datadog OTel Collector.

## Audit log

When `COOKER_AUDIT_ENABLED=true` (default in production), every authenticated mutating call produces one structured event:

```json
{
  "time":   "2026-05-12T10:30:00Z",
  "subject":"alice@example.com",
  "method": "PUT",
  "route":  "/api/v1/environments/:id/secrets/:key",
  "status": 200,
  "latency_ms": 47,
  "client_ip": "10.0.1.5"
}
```

Fields:

| Field | What |
|---|---|
| `time` | RFC 3339 UTC. |
| `subject` | OIDC `sub` claim (or `dev-user` in dev mode). |
| `email` | OIDC `email` claim, if present. |
| `method` | HTTP method. |
| `route` | Gin matched template — **never the concrete URL with IDs**. |
| `status` | HTTP status code. |
| `latency_ms` | Server-side handler latency. |
| `client_ip` | From `c.ClientIP()`. |

**Bodies are never captured.** The middleware does not read request or response bodies, so secret-bearing routes are safe by construction.

### Destination

Two sinks, selected via `COOKER_AUDIT_DESTINATION`:

| Value | Where it writes |
|---|---|
| `stdout` *(default)* | Goes to the container's stderr; your log shipper picks it up. |
| `file` | `COOKER_AUDIT_FILE_PATH`. The file is opened append-only and never rotated; use a sidecar log shipper that rotates it. |

The audit writer is async with drop-on-backpressure. `cooker_audit_events_dropped_total` increments when the buffer fills.

## Suggested dashboards

For Prometheus/Grafana:

- Request rate by route (`sum by(route)(rate(cooker_http_requests_total[1m]))`).
- p50 / p95 / p99 latency by route (`histogram_quantile(0.95, ...)`).
- Error rate (`sum(rate(cooker_http_requests_total{status=~"5.."}[1m])) / sum(rate(cooker_http_requests_total[1m]))`).
- Audit events dropped (`rate(cooker_audit_events_dropped_total[5m])`) — should be 0.

> **Not yet shipped:** a starter Grafana dashboard JSON (`deploy/dashboards/cooker-overview.json`). Tracked under `shipping-go 30-90d` #17.

## What to alert on

| Alert | Threshold | What it usually means |
|---|---|---|
| Error rate sustained > 1% | 5 min | Real bug or a downstream-cluster issue. |
| p99 latency > 5s | 5 min | DB contention or large pipeline JSON. |
| `cooker_audit_events_dropped_total` > 0 | any | Audit-sink backpressure; investigate disk / SIEM ingestion. |
| Readiness probe failing | 5 min | Postgres unreachable. |
| Pod restart count > 0 in last 24h | — | OOM, panic, or pre-stop hook failure. |

## Cross-references

- **[`SECURITY.md` § Audit logging](../../../SECURITY.md#audit-logging)** — what the audit log promises.
- **[`docs/shipping-go.md` § 3](../../shipping-go.md#3-observability-that-ships-well)** — the gap analysis vs the rest of the Go OSS world.
- **[Reference: env vars](../reference/env-vars.md#observability)** — observability variables enumerated.
