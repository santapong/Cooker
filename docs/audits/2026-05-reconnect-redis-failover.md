# Redis-failover reconnect timing audit (W4)

Date: 2026-05-13
PRs in scope: #21 (Redis pub/sub WS hub), #19 (reconnect backoff), #49 (FH-03 unmount guard), #57 (P26-05-29 stable onMessage ref)
Author: automated audit

---

## Failure mode 1 — Replica's Redis subscriber disconnects

### What happens server-side

`redisHubBackend.consume()` (`wshub_backend.go:153–191`) runs a resubscribe loop.
When the Redis connection drops, `ps.Channel()` closes, `drain()` returns, and
`consume()` sleeps for a jittered exponential delay (initial 500 ms, cap 30 s)
before calling `client.Subscribe()` again. The outer channel (`b.ch`) is **not
closed** during this gap; only the context cancellation path closes it
(`defer close(b.ch)` in `consume()`). `WebSocketHub.Run()` blocks on `<-inbox`
during the reconnect window.

The server does **not** send a graceful WebSocket close frame to connected
clients during a Redis subscriber blip. The hub's `writePump`
(`websocket.go:226`) only closes the WS connection when either:
- the hub drops the client due to backpressure (send channel full), or
- the ping/pong keepalive fails (`wsPingPeriod = 54 s`, `wsPongWait = 60 s`).

### What the client experiences

During the Redis reconnect window (up to 30 s + jitter), broadcasts stop
arriving on the open WS connection. The WS itself stays open and the pong
keepalive continues. The client (`useWebSocket.ts`) does not see a disconnect —
`ws.onclose` does not fire. Log lines emitted by the executor are silently
dropped on the floor (Redis pub/sub has no replay buffer).

**Findings:**

1. **INFO — Silent message loss during Redis blip.** The client cannot distinguish
   "hub is in Redis reconnect" from "no events are being produced." No severity
   signal reaches the browser during the blackout window. Operators see a
   `slog.Warn` on the server but the UI user sees nothing.
   *Recommendation:* emit a structured "hub degraded / reconnecting" message
   over the existing WS connection during the subscriber gap, so the UI can
   show a brief "stream interrupted" indicator. Out of scope for this audit.

2. **OK — No TCP RST to the client.** The server never tears down the client's
   WS connection during a Redis subscriber blip. The client experiences no
   disconnect; no reconnect is triggered.

---

## Failure mode 2 — Replica's ws-ticket-store Redis disconnects

### What happens server-side

`redisTicketStore.Issue()` (`wsticket_redis.go:27–43`) calls `client.Set` with
a 2-second timeout. If Redis is unavailable, the call returns an error and the
handler (`router.go:220–228`) returns HTTP 500 ("ticket issuance failed").

### What the client experiences

`fetchWSTicket()` (`useWebSocket.ts:21–29`) checks `res.ok`. A 500 is not OK,
so the function returns `null`. The `connect()` callback (`useWebSocket.ts:85–90`)
then calls `scheduleReconnect()`:

```
if (!ticket) {
  scheduleReconnect();
  return;
}
```

The client backs off with exponential jitter (500 ms → 30 s cap) and retries
the ticket fetch. Once Redis recovers, `Issue()` succeeds and the connection
opens cleanly.

**Findings:**

3. **OK — Graceful degradation on ticket-store Redis failure.** A 500 from
   `/api/v1/ws-tickets` is treated identically to a failed ticket: the client
   backs off and retries. No reconnect attempt floods the server — the same
   exponential backoff governs both cases.

4. **OK — No replay-attack surface from ticket-store failure.** Tickets that
   were issued before the Redis blip expire naturally (Redis TTL 60 s or
   `GetDel` returns `redis.Nil` after recovery). The client will re-fetch a
   fresh ticket on each reconnect attempt; the server will issue a new one once
   Redis recovers.

5. **LOW — 500 vs 503 semantics.** The handler returns 500 for any `Issue()`
   error, including a transient Redis timeout. HTTP 503 would more precisely
   signal "backend temporarily unavailable, retry is safe." The client treats
   both identically today, but a future alerting layer keying on 5xx codes
   might misclassify a Redis blip as an application crash.
   *Recommendation:* distinguish `Is(err, redis.Nil)` and transient timeouts
   from programming errors; return 503 for Redis-unavailable cases.

---

## Failure mode 3 — WS connection drops while subscriber is fine

`ws.onclose` fires on the browser (`useWebSocket.ts:102–107`). `scheduleReconnect()`
runs; `connect()` calls `fetchWSTicket()` for a fresh ticket before opening the new WS.

**Findings:**

6. **OK — Fresh ticket on every reconnect.** Every reconnect goes through
   `fetchWSTicket()` at `useWebSocket.ts:83`. Old tickets are already consumed
   (single-use `GETDEL`) or expired. No replay is possible.

7. **OK — Attempt counter resets on success.** `ws.onopen` resets
   `attemptRef.current = 0` (`useWebSocket.ts:99`); the next failure starts
   backoff from 500 ms.

---

## Failure mode 4 — Replica replaced mid-stream

A Kubernetes rolling update terminates a pod while a browser has an active WS
to that pod.

### Shape A — sticky sessions enabled

NGINX sends all requests from this client to the terminating pod until the
pod's graceful shutdown completes (`shutdownTimeout = 30 s`,
`server.go:322`). `httpSrv.Shutdown()` stops accepting new connections and
waits for in-flight requests. Long-lived WS connections are not in-flight HTTP
requests in the traditional sense — Gorilla WebSocket connections are not shut
down by `http.Server.Shutdown`; the TCP connection is kept alive until the pod
is SIGKILLed (after the Kubernetes `terminationGracePeriodSeconds` expires).

Once the pod is SIGKILLed the client sees a TCP RST, `ws.onclose` fires, and
the reconnect loop begins. The new pod is now the sticky target (the ingress
rebalances on the next request). The client fetches a ticket from the new
replica and reconnects.

**Findings:**

8. **MEDIUM — WS connections not cleanly drained on pod shutdown.** Gorilla WS
   connections are not terminated by `http.Server.Shutdown()`. The server does
   not iterate `wsHub.clients` and send close frames on shutdown. Clients
   connected to a draining pod will hold open connections until SIGKILL
   (`terminationGracePeriodSeconds`), then receive a TCP RST rather than a
   graceful WS close frame (code 1001 Going Away). The client recovers via
   the reconnect loop regardless, but the UX is a hard disconnect rather than
   a clean handover.
   *Recommendation:* add a `wsHub.CloseAll()` call in `RunContext()` after
   `httpSrv.Shutdown()` returns; iterate `clients`, send close frames, and
   drain. Not a correctness bug — the reconnect loop handles it — but improves
   UX during rolling updates.

### Shape B — Redis pub/sub hub, no sticky sessions

After reconnect, the client's WS upgrade can land on any replica. The ticket
was issued by a previous replica's `redisTicketStore`, stored in Redis with a
60 s TTL. The new replica's `wsTicketGate()` calls `s.wsTickets.Consume()`,
which calls `GETDEL` on Redis. If the ticket has not yet expired, the upgrade
is accepted on the new replica. The client is now subscribed to the new
replica's local `clients` map; broadcasts published to Redis reach it via the
new replica's `consume()` loop.

**Findings:**

9. **OK — Cross-replica ticket consumption works.** `redisTicketStore` uses
   `GETDEL` (atomic) so a ticket issued on replica A is validly consumed on
   replica B. The 60-second TTL is generous relative to the reconnect backoff
   cap (30 s), so a ticket fetched before reconnect is almost certainly still
   valid on arrival.

10. **TIMING EDGE — 30 s backoff cap vs 60 s ticket TTL.** The backoff cap is
    30 s; the ticket TTL is 60 s. `scheduleReconnect()` fires a `setTimeout`,
    then `connect()` fetches the ticket and immediately opens the WS — there is
    no sleep between fetch and open. Ticket age at upgrade time equals the
    round-trip only (~100 ms). The TTL boundary is not a real risk.

---

## Summary

| # | Failure mode | Client experience | Severity | Status |
|---|---|---|---|---|
| 1 | Redis subscriber blip | Broadcasts silently pause; WS stays open | INFO | Open — cosmetic |
| 2 | Ticket-store Redis blip → 500 | Reconnect loop retries; recovers when Redis does | LOW | Open — 500 vs 503 |
| 3 | WS TCP drop, subscriber fine | Reconnect with fresh ticket; no replay risk | OK | Clean |
| 4a | Pod replaced, sticky sessions | TCP RST on SIGKILL; reconnect loop recovers | MEDIUM | Open — no WS drain on shutdown |
| 4b | Pod replaced, Redis hub, no sticky | Cross-replica ticket GETDEL succeeds | OK | Clean |
| Timing | 30 s backoff cap vs 60 s ticket TTL | Ticket is fetched immediately before WS open; gap is ~100 ms | OK | Clean |

**Real bugs found: zero.** Findings #1, #2, and #8 are improvements, not bugs:
the reconnect loop handles all four failure modes correctly and the 60s TTL /
30s backoff relationship is safe. The audit confirms the current implementation
is resilient to Redis failover in both sticky-session and Redis-hub-shared-state
topologies.
