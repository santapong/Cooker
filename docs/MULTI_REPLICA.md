# Multi-replica deployment

Cooker is single-replica by default. Running 2+ replicas behind a load balancer works, but you have to choose between two strategies for two pieces of per-process state: the **rate limiter** (PR H) and the **WebSocket ticket store** (PR F). This document covers both.

> Backlog reference: P3.

## What breaks without action

| Component | Per-process state | Failure mode under naive multi-replica |
|---|---|---|
| Rate limiter (`backend/internal/rate/`) | In-memory token bucket per user | Limits effectively N× looser than configured (one bucket per replica). Bursty users can pin a single replica. |
| WebSocket ticket store (`backend/internal/server/wsticket.go`) | In-memory single-use tickets | Client opens an HTTP request to replica A, receives a ticket; the WS upgrade hits replica B → ticket unknown → 401. Manifests as random sign-in / live-log failures. |

Both components ship in-process and per-replica today. PR F's commit message explicitly calls this out.

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

## Strategy 2 — Redis-backed shared state (open backlog item)

Swap both per-process stores for Redis-backed equivalents:

- Rate limiter: `redis_rate` package (`github.com/go-redis/redis_rate/v10`) sliding-window over Redis keys.
- WS ticket store: SETEX with the ticket as the key, single-use enforced by `GETDEL`.

This is **not implemented today.** Tracked in backlog `P3`. Until then, sticky sessions are the supported multi-replica path.

## Verification

After applying sticky-session annotations:

1. `curl -i https://cooker.example.com/health` twice; the response should set the `COOKER_AFFINITY` cookie on the first request and reflect it on the second.
2. Open the UI in two different browser profiles; each should land on a stable replica (check the `Server-Pod` debug header if your ingress sets one, or inspect WS connections via `kubectl get pods` + `kubectl logs`).
3. Trigger a pipeline run; the live log stream should reconnect cleanly across page reloads.
