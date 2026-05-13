# useStageLogs reconnect during active run (W5)

Date: 2026-05-13
Transport-layer coverage: PR #72 (`docs/audits/2026-05-reconnect-redis-failover.md`)
Scope of this audit: application-level subscription state across WS reconnects

---

## Q1 — Subscription registration

**How does `useStageLogs` subscribe to stage log events?**

`useStageLogs` (`frontend/src/hooks/useStageLogs.ts:93–114`) constructs the URL:

```
/ws/runs/<runId>/stages/<stageId>/logs
```

and passes it to `useWebSocket` with `autoConnect: enabled && !!wsUrl`. There is
no explicit subscribe message sent over the wire after the connection opens.
Subscription is implicit by URL path: the server upgrades the WS and registers
the connection directly into the hub channel `stage-logs:<runId>:<stageId>`.

Server-side: `router.go:246–248` routes `GET /ws/runs/:runId/stages/:stageId/logs`
to `wsHub.HandleStageLogs`. That method (`wshub_logs.go:24–26`) calls the shared
`handleConnection` helper (`websocket.go:185–203`) with the channel string
`stageLogChannel(runId, stageId)` (`wshub_logs.go:11–13`). `handleConnection`
upgrades the socket, builds a `*Client`, and enqueues it on `h.register`
(`websocket.go:199`). `Run()` picks it up and inserts it into
`h.clients[channel]` (`websocket.go:100–105`).

**Conclusion:** subscription is URL-path-implicit. No subscribe frame is sent;
no server-side record exists beyond the `clients` map entry created at upgrade.

---

## Q2 — Reconnect handling (GAP — HIGH severity)

**When `useWebSocket` reconnects with a new `WebSocket` instance, are existing
subscriptions replayed?**

`useWebSocket` (`frontend/src/hooks/useWebSocket.ts:102–106`) fires `ws.onclose`
on the old connection, which calls `scheduleReconnect()`. The reconnect path
calls `connect()` again (`useWebSocket.ts:74`), which:

1. Fetches a fresh ticket via `POST /api/v1/ws-tickets`.
2. Constructs a new `WebSocket` to the same URL.
3. Assigns it to `wsRef.current`.

The URL passed to `useWebSocket` comes from `useStageLogs.ts:93–95` and is
derived from `runId` / `stageId` in scope at render time. As long as the
component is still mounted and those props have not changed, the reconnect
opens to the same `/ws/runs/<runId>/stages/<stageId>/logs` path — which the
server treats as a fresh subscription.

**This is correct.** The URL-path-implicit model means a reconnect to the same
URL automatically re-establishes the channel subscription on the new `*Client`
instance. There is no separate re-subscribe step needed and none is missing.

However, there is a **silent log gap**:

- Lines emitted by the executor between the old connection closing and the new
  connection opening are not buffered by the server hub. Redis pub/sub has no
  replay (`wshub_backend.go:94–96` documents this explicitly). The backfill REST
  call (`pipelineApi.getStageLogs`, `useStageLogs.ts:65–88`) is only executed on
  mount or when the `(pipelineId, runId, stageId)` triple changes — it is NOT
  re-issued on WS reconnect.

**Gap — HIGH: log lines produced during the reconnect window are lost.**
The buffer is not reset on reconnect (correct), but lines emitted after the old
WS closed and before the new one opens are permanently missing. The user sees
frozen output until fresh lines arrive. For a busy stage this is an invisible
multi-second (up to 30 s backoff cap) hole in the log stream.

**Proposed remediation:** on reconnect, `useStageLogs` should re-fetch
`getStageLogs` and merge with the in-memory buffer. This requires `useWebSocket`
to surface a "reconnected" signal — either an `onReconnect` callback prop or an
exported `reconnectCount` counter watched via `useEffect`.

---

## Q3 — Server-side subscription state

**Does the server-side hub remember which stages a client is subscribed to
across a Redis reconnect or replica replacement?**

The hub's `clients` map (`websocket.go:47`) is **in-process only**. Each
`*Client` carries a single `channel` string (`websocket.go:27`) set at
connection time. There is no persistent subscription registry in Redis or any
other store.

When a Redis subscriber blip occurs (`wshub_backend.go:153–191`), the hub's
`Run()` loop pauses on `<-inbox` but the existing `clients` entries remain
intact. The `*Client.send` channel drains normally; broadcasts that arrive after
the Redis reconnect resume delivery to the same `*Client` pointers. No
subscription is lost during a Redis blip — the only loss is the messages
published during the gap (no replay buffer).

When a pod is replaced (Failure mode 4 in PR #72), the old pod's entire
`clients` map is discarded on SIGKILL. The client must reconnect to a new pod,
which registers a new `*Client` with a fresh channel entry. Subscription state
is recovered implicitly by the reconnect URL (Q2 above).

**Conclusion:** server-side state is ephemeral per-process. It does not persist
across pod restarts. That is correct by design; recovery depends entirely on the
client reconnecting to the same URL.

---

## Q4 — Cross-tab consistency

**Two tabs open on the same run: if one tab reconnects, does the other tab see
the same stream?**

Each browser tab holds its own `*Client` entry in the hub's `clients` map (one
per WS connection). The hub broadcasts to all entries for a given channel
simultaneously (`websocket.go:127–134`). Both tabs receive the same messages
independently.

When one tab reconnects, it opens a new WS upgrade, which registers a new
`*Client`. The other tab's `*Client` is undisturbed — its `channel` entry in the
map is unchanged. The reconnecting tab's gap (Q2) affects only that tab's
in-memory buffer; the other tab continues to accumulate lines without
interruption.

**Conclusion:** cross-tab consistency is correct. Tabs are independent
subscribers; one tab's reconnect has no effect on the other.

---

## Summary

| # | Question | Finding | Severity |
|---|---|---|---|
| Q1 | Subscription registration | URL-path-implicit; no subscribe frame; correct | OK |
| Q2 | Reconnect / re-subscribe | WS reconnects to same URL automatically; **but lines during gap are lost** because backfill is not re-fetched on reconnect | **HIGH — silent gap** |
| Q3 | Server-side state | Ephemeral per-process; recovers on reconnect by URL; no replay | OK (by design) |
| Q4 | Cross-tab consistency | Independent `*Client` entries; one tab's reconnect doesn't affect the other | OK |

**Real gap found: one (Q2).** Log lines emitted between WS drop and reconnect
are silently lost. The backfill REST call is issued only on mount, not on
reconnect. For a busy stage emitting hundreds of lines per second during a 1–5 s
reconnect window, this is a visible hole in the log output.

**Proposed fix (not in scope for this audit):** expose `onReconnect` or a
`reconnectCount` signal from `useWebSocket`; `useStageLogs` re-fetches
`getStageLogs` on each reconnect and merges with the in-memory buffer. Candidate
backlog item: `useStageLogs` backfill-on-reconnect (LOW complexity, HIGH UX
value during flapping networks or rolling deploys).
