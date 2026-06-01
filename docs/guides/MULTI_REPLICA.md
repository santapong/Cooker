# Multi-replica deployment

Cooker can run 2+ replicas safely. The default Helm chart now ships **Redis-backed** rate limiter, WS ticket store, and WS broadcast hub, so you don't need sticky sessions for correctness. Sticky sessions remain a supported fallback when you can't (or don't want to) run Redis.

> Backlog reference: P3.

## What breaks without action

| Component | Per-process state | Failure mode under naive multi-replica + memory backend |
|---|---|---|
| Rate limiter | In-memory token bucket per user | Limits effectively N× looser than configured. Bursty users can pin one replica. |
| WS ticket store | In-memory single-use tickets | Client receives a ticket from replica A; WS upgrade hits replica B → 401. Random sign-in / live-log failures. |
| WS broadcast hub | In-memory `clients map` + channel | A run-status broadcast on replica A is invisible to a UI tab pinned to replica B. |

Cooker's `Config.Validate()` refuses production boot when `replicaCount>1` and any of those is set to `memory` without `stickySessions=true`.

## WS broadcast topology (Redis backend)

When `wsHub.backend=redis`, every `Broadcast()` is published to the Redis topic `cooker:ws:broadcast`. Each replica subscribes to that topic and fans the message out to its **local** clients via the existing in-process `clients` map. Per-client subscription state stays per-replica; only the broadcast message itself crosses Redis.

This means:
- Hot path is unchanged for browsers connected to replica A — they still receive their messages from replica A's writePump.
- A pod restart drops only that replica's clients (they reconnect via the existing exponential-backoff hook).
- Redis is not on the critical path of the WS upgrade itself — it only carries broadcasts. A Redis blip silences broadcasts (visible as missing log lines) but the UI itself stays up.

## Strategy 1 — Sticky sessions at the ingress (recommended for now)

Pin each client to a single replica. The cookie travels through the WS upgrade, so the ticket lookup hits the same process that issued it.

### NGINX Ingress (Kubernetes)

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: cooker
  annotations:
    # Pin sessions to a backend pod via cookie. Default cookie name
    # is INGRESSCOOKIE; override if you already use that name.
    nginx.ingress.kubernetes.io/affinity: cookie
    nginx.ingress.kubernetes.io/affinity-mode: persistent
    nginx.ingress.kubernetes.io/session-cookie-name: COOKER_AFFINITY
    nginx.ingress.kubernetes.io/session-cookie-max-age: "3600"
    # WebSocket support
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
spec:
  ingressClassName: nginx
  tls:
    - secretName: cooker-tls
      hosts: [cooker.example.com]
  rules:
    - host: cooker.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: cooker
                port:
                  number: 8080
```

Key settings:
- `affinity: cookie` and `session-cookie-name` enable cookie-based sticky sessions.
- `session-cookie-max-age` should comfortably exceed the WS ticket TTL (60s) and any human session length you care about.
- The `proxy-*-timeout` values matter for long-lived WS connections (live build logs).

### Other ingress controllers

| Ingress | Stickiness annotation |
|---|---|
| AWS ALB | `alb.ingress.kubernetes.io/target-group-attributes: stickiness.enabled=true,stickiness.lb_cookie.duration_seconds=3600` |
| Traefik | `traefik.ingress.kubernetes.io/service.sticky.cookie: "true"` |
| HAProxy | `haproxy.org/cookie-persistence: SERVERID insert indirect nocache` |
| Envoy / Istio | `DestinationRule { trafficPolicy.loadBalancer.consistentHash.httpCookie }` |

### What this does NOT solve

Sticky sessions do **not** synchronize the rate-limiter state across replicas. A coordinated burst that lands on multiple sticky cookies (multiple browser sessions, scripted clients) still gets N× the configured limit. For most setups this is acceptable; if not, see Strategy 2.

## Strategy 2 — Redis-backed shared state (default)

The chart now defaults to Redis for all three components:

- Rate limiter: `redis_rate/v10` GCRA sliding window.
- WS ticket store: `SETEX` + atomic `GETDEL` on Redis 6.2+.
- WS broadcast hub: pub/sub on `cooker:ws:broadcast`.

Toggle via `wsHub.backend`, `wsTicket.backend`, `rateLimit.backend` (each accept `memory` or `redis`). A single `redis.url` is shared across all three. With these three set to `redis`, sticky sessions are unnecessary for correctness (you may still want them for cache locality).

## Verification

After applying sticky-session annotations:

1. `curl -i https://cooker.example.com/health` twice; the response should set the `COOKER_AFFINITY` cookie on the first request and reflect it on the second.
2. Open the UI in two different browser profiles; each should land on a stable replica (check the `Server-Pod` debug header if your ingress sets one, or inspect WS connections via `kubectl get pods` + `kubectl logs`).
3. Trigger a pipeline run; the live log stream should reconnect cleanly across page reloads.
