# Cooker observability

Pre-built monitoring assets for Cooker:

| File | What it is |
|---|---|
| `dashboards/cooker-overview.json` | Grafana dashboard — HTTP rate/latency/5xx, jobqueue depth, stage duration, and the four SPOF resilience counters. |
| `prometheus/cooker-rules.yaml` | Prometheus Operator `PrometheusRule` CRD with the Cooker alert set. |

All Cooker series are exposed on the `/metrics` endpoint of the HTTP port
(`8080`) when `COOKER_METRICS_ENABLED=true`. The Helm chart sets this by
default (`.Values.metrics.enabled=true`); the raw manifests set it as a
literal env var. The Go binary's own default is `false`, so make sure the
env var is set if you bypass both deploy paths.

> Note: the HTTP 5xx series is selected on the **`status`** label
> (`status=~"5.."`), not a `code` label. See
> `backend/internal/observability/observability.go`.

---

## Import the Grafana dashboard

The dashboard references a templated Prometheus datasource variable named
`$datasource`, so it works against any Prometheus datasource without editing
the JSON.

**UI:** Dashboards → New → Import → Upload JSON file → select
`dashboards/cooker-overview.json` → pick your Prometheus datasource for the
`Datasource` variable → Import.

**Grafana provisioning (file-based):** drop the JSON into a provisioned
dashboards folder, e.g.:

```yaml
# /etc/grafana/provisioning/dashboards/cooker.yaml
apiVersion: 1
providers:
  - name: cooker
    folder: Cooker
    type: file
    options:
      path: /var/lib/grafana/dashboards/cooker
```

then mount `cooker-overview.json` at
`/var/lib/grafana/dashboards/cooker/`.

---

## Apply the alert rules

### Prometheus Operator (kube-prometheus-stack)

```sh
kubectl apply -n monitoring -f prometheus/cooker-rules.yaml
```

The `PrometheusRule` carries `role: alert-rules`. Confirm this matches your
Prometheus' `spec.ruleSelector` (kube-prometheus-stack commonly selects on
`release: <stack-release>` — add that label if so). The
`CookerReadinessFailing` rule depends on **kube-state-metrics**
(`kube_pod_status_ready`); it ships with kube-prometheus-stack.

**Via the Helm chart instead:** the chart can render the same rule for you —
`--set metrics.prometheusRule.enabled=true` (requires the Operator CRDs in
the cluster). Likewise `--set metrics.serviceMonitor.enabled=true` renders a
`ServiceMonitor` that scrapes the `http` port at `/metrics`.

### Plain Prometheus (no Operator)

Copy the `spec.groups:` block from `prometheus/cooker-rules.yaml` into a file
that your Prometheus `rule_files` already references, e.g.:

```yaml
# prometheus.yml
rule_files:
  - /etc/prometheus/rules/cooker.rules.yaml
scrape_configs:
  - job_name: cooker
    metrics_path: /metrics
    static_configs:
      - targets: ["cooker.cooker.svc.cluster.local:8080"]
```

`/etc/prometheus/rules/cooker.rules.yaml` then holds just the two groups
(`cooker-resilience`, `cooker-http`) under a top-level `groups:` key — that
is exactly the `spec` contents of the CRD, minus the Kubernetes wrapper.

---

## Alerts at a glance

| Alert | Fires when | Severity |
|---|---|---|
| `CookerDBConnectionErrors` | `rate(cooker_db_connection_errors_total[5m]) > 0` for 5m | page |
| `CookerRedisConnectionErrors` | `rate(cooker_redis_connection_errors_total[5m]) > 0` for 5m | page |
| `CookerJWKSFetchFailures` | `rate(cooker_jwks_fetch_failures_total[5m]) > 0` for 10m | warning |
| `CookerOrphanedRunsHigh` | `increase(cooker_pipeline_runs_orphaned_total[1h]) > 5` | warning |
| `CookerHTTP5xxRate` | 5xx ratio > 5% for 10m | page |
| `CookerReadinessFailing` | a `cooker.*` pod NotReady for 5m | page |

Runbook entries: `docs/guides/RUNBOOK.md`.
