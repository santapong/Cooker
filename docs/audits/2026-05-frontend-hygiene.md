# Frontend Hygiene Audit — Zustand + Hooks — 2026-05

**Scope:** `frontend/src/stores/**`, `frontend/src/hooks/**`. Four classes of issues: `localStorage` violations, hardcoded backend URLs, `useEffect` cleanup misses, and `useWebSocket` reconnect race patterns.
**Method:** Static source reading. Every finding cites `file:line` against `main` at audit time.
**Lint baseline:** `npm run lint` passes with 6 pre-existing warnings (all `react-refresh/only-export-components` in `auth/OIDCProvider.tsx`, `components/ui/atoms.tsx`, `theme/ThemeProvider.tsx`). Zero errors.
**TypeScript baseline:** `npx tsc --noEmit` exits clean.

**Cross-references:**
- `2026-05-perf-and-optimization.md` §P26-05-29 — `onMessage` ref pattern (WS churn). Noted below; not duplicated.
- `2026-05-perf-and-optimization.md` §P26-05-25 — Zustand store consumed without selectors. Out of scope for this audit; not re-stated.

---

## Class 1 — `localStorage` outside `frontend/src/auth/`

CLAUDE.md rule: "No `localStorage` outside `auth/` — token storage is owned by `oidc-client-ts`; everything else uses Zustand."

### FH-01 — `uiStore` uses Zustand `persist` middleware which defaults to `localStorage`

- **Severity:** Low (no security impact; pure UI preferences)
- **File:** `frontend/src/stores/uiStore.ts:2,19-34`
- **Detail:** `uiStore` imports `persist` from `zustand/middleware` and calls it without supplying a `storage` option. Zustand's `persist` defaults to `window.localStorage` (confirmed in `node_modules/zustand/middleware.js:472-473`). The persisted slice is `{ mode: UIMode, themeMode: ThemeMode }` — the key written is `"cooker-ui"`. No tokens or PII are stored; the violation is purely against the architectural rule.
- **Why it matters:** The rule exists to prevent token-handling code from accidentally spreading across the codebase. Using `localStorage` anywhere outside `auth/` erodes that invariant even for benign data, and makes audits harder.
- **Fix:** Provide an explicit storage backend. Either:
  - `storage: createJSONStorage(() => localStorage)` — explicit, still `localStorage`, but opt-in and visible.
  - `storage: createJSONStorage(() => sessionStorage)` — doesn't survive tabs, consistent with Zustand-in-memory spirit.
  - Or accept the violation and carve out a documented exception for UI preference persistence.
- **Effort:** XS — one line addition to the `persist(...)` call.

---

## Class 2 — Hardcoded backend URLs

CLAUDE.md rule: "No backend URLs in components — all HTTP goes through `api/`."

### FH-02 — `useWebSocket` hardcodes `/api/v1/ws-tickets` and calls raw `fetch` directly

- **Severity:** Medium
- **File:** `frontend/src/hooks/useWebSocket.ts:21-29`
- **Detail:** `fetchWSTicket()` is a module-level async function that calls `fetch('/api/v1/ws-tickets', { method: 'POST', headers })`. It manually injects the Bearer token by calling `getAccessToken()` and does not go through `api/client.ts`. The URL `/api/v1/ws-tickets` is the only backend URL hardcoded outside `api/`.
- **Why it matters:** Two concrete problems:
  1. The `API_BASE = '/api/v1'` constant in `api/client.ts` is the single source of truth for the API prefix. If the prefix ever changes (e.g., versioning to `/api/v2`), `useWebSocket.ts` will be missed.
  2. The raw `fetch` bypasses the standard 401 handler. `api/client.ts` calls `triggerSignIn()` on 401; `fetchWSTicket` returns `null` on a non-ok response and schedules a backoff retry instead. An expired session during ticket fetch silently backoffs rather than redirecting to sign-in.
- **Fix:** Move `fetchWSTicket` into `api/` (e.g., `api/wsTickets.ts`) and call it through `api/client.ts`. The 401 path then falls through to `triggerSignIn()` automatically. The hook imports the typed function.
- **Effort:** S — ~20 lines relocated; no logic change.

---

## Class 3 — `useEffect` cleanup misses

### FH-03 — `useWebSocket.connect()` does not check `closedByCallerRef` after the `await fetchWSTicket()` yield point — orphaned WebSocket on fast unmount

- **Severity:** High — this is the "will show up in Sentry" pick (see bottom)
- **File:** `frontend/src/hooks/useWebSocket.ts:62-111`
- **Detail:** `connect()` is an `async` function. Its body executes as two logical segments separated by the `await fetchWSTicket()` call at line 71. The first segment (lines 62-71) runs synchronously and sets `closedByCallerRef.current = false` at line 70. The second segment (lines 72-107) runs after the network round-trip resolves.

  The bug: if the component **unmounts** while `fetchWSTicket()` is in-flight, `disconnect()` runs (line 126-133): it sets `closedByCallerRef.current = true`, clears `timerRef`, and calls `wsRef.current?.close()`. At this point `wsRef.current` is still `null` (the WebSocket hasn't been created yet), so the close is a no-op. When `fetchWSTicket` then resolves with a valid ticket, `connect()` resumes past line 72 without re-checking `closedByCallerRef`. It proceeds to create `new WebSocket(wsUrl)` at line 83, assigns it to `wsRef.current` at line 107, and returns. The effect-cleanup has already finished; no further cleanup runs. The WebSocket is never closed.

  **Reproduction sequence:**
  1. Component mounts with `autoConnect: true`.
  2. `useEffect` fires, `connect()` starts, `fetchWSTicket()` is awaited.
  3. User navigates away before the ticket response arrives (fast navigation, flaky network making ticket fetch slow).
  4. React unmounts the component; `disconnect()` runs (cleanup from `useEffect` line 145).
  5. Ticket arrives; `connect()` creates an orphaned WebSocket.
  6. That socket receives server-sent messages; `onMessage` is called on the stale closure.
  7. `setConnected(true)` (line 87) is called on an unmounted component — React 18 no-ops this but it's a leak signal. In older React it would throw.

- **Fix:** Add a `closedByCallerRef.current` guard immediately after the `await`:

  ```ts
  const ticket = await fetchWSTicket();
  if (closedByCallerRef.current) return;   // <-- add this line
  if (!ticket) {
    scheduleReconnect();
    return;
  }
  ```

  One line. The guard mirrors the check already in `scheduleReconnect` (line 115) and `ws.onclose` (line 91).

- **Effort:** XS — one line.

### FH-04 — `useStageLogs` first `useEffect` has no cleanup but is safe

- **Severity:** Informational
- **File:** `frontend/src/hooks/useStageLogs.ts:56-60`
- **Detail:** The first effect only calls `setLines([])`, `setBackfillLoaded(false)`, `setTruncated(false)` — pure state resets. No subscriptions, intervals, or listeners are opened. No cleanup return is required. Flagged for completeness; no action needed.

---

## Class 4 — `useWebSocket` reconnect race patterns

### FH-05 — `onMessage` identity churn triggers spurious disconnect/reconnect on every parent re-render

- **Severity:** Medium
- **File:** `frontend/src/hooks/useWebSocket.ts:62,111`, `frontend/src/hooks/useStageLogs.ts:99`
- **Detail:** Already documented as `P26-05-29` in `2026-05-perf-and-optimization.md`. Summarised here for completeness as it directly affects reconnect stability: `connect`'s `useCallback` lists `onMessage` as a dependency. `useStageLogs` passes an inline arrow to `useWebSocket` (line 99), whose identity changes on every render. The chain: new render → new arrow → new `connect` identity → `useEffect([autoConnect, connect, disconnect])` re-fires → `disconnect()` + `connect()` executed. Under the re-render storms described in `P26-05-25`, this manifests as repeated WS teardown/reconnect cycles visible as WebSocket `CLOSE` frames in devtools.
- **Fix:** Stash `onMessage` in a `useRef` inside `useWebSocket`; reference the ref inside the `ws.onmessage` handler; drop `onMessage` from `useCallback` deps. (Detailed in `P26-05-29`.)
- **Effort:** S.

### FH-06 — Ticket lifetime is safe relative to maxDelay but the auth expiry path silently backoffs instead of redirecting

- **Severity:** Low-Medium
- **File:** `frontend/src/hooks/useWebSocket.ts:21-29,72-77`
- **Detail:** Tickets are 60-second single-use tokens; `maxDelay` is 30 seconds. At steady-state retries the hook attempts a reconnect every ≤30 seconds, fetching a fresh ticket each time. The ticket is never reused across attempts. From a timing standpoint, ticket lifetime versus reconnect interval is not a problem.

  The related problem (see FH-02): when `fetchWSTicket` receives a `401` (session expired), `fetchWSTicket` returns `null`. `connect()` then calls `scheduleReconnect()`, which schedules another backoff attempt. The user is never redirected to sign in. They remain on the page with a perpetually-reconnecting-but-failing WebSocket and no UI feedback about why. The HTTP API's 401 path (`api/client.ts:21-24`) correctly calls `triggerSignIn()`; the WS ticket path does not share this behaviour.

  In practice: after `automaticSilentRenew` fails (see `onSilentRenewError` in `OIDCProvider.tsx:260-265`), a toast is shown, but `useWebSocket` will keep retrying the ticket endpoint with an expired token until `maxAttempts` is reached (default: `Infinity`).

- **Fix:** In `fetchWSTicket`, if the response is `401`, call `triggerSignIn()` and return `null`. Or, move the function into `api/client.ts` which already handles this (see FH-02).
- **Effort:** XS (two lines in `fetchWSTicket`) — or superseded by the FH-02 fix.

### FH-07 — No guard against a connection going half-open before `onopen` fires

- **Severity:** Low
- **File:** `frontend/src/hooks/useWebSocket.ts:83-107`
- **Detail:** `connect()` unconditionally creates `new WebSocket(wsUrl)` without checking if `wsRef.current` already holds an open or connecting socket. This is safe in the current reconnect flow (the previous socket fired `onclose` before `scheduleReconnect` runs, and the `useEffect` cleanup calls `disconnect()` which nullifies `wsRef.current`). However, if `connect()` is called imperatively by the caller (via the returned `connect` function) while an existing connection is open, the old socket is silently orphaned — `wsRef.current` is overwritten and the old socket is never closed.

  Currently no caller invokes the returned `connect` while connected; this is a latent issue rather than an active bug.

- **Fix:** At the start of the second segment of `connect()` (after the `await`), close any existing socket before creating the new one:
  ```ts
  wsRef.current?.close();
  wsRef.current = null;
  ```
- **Effort:** XS.

---

## Summary table

| ID | Class | Severity | File:line | Fix | Effort |
|---|---|---|---|---|---|
| FH-01 | localStorage outside auth | Low | `stores/uiStore.ts:19` | Explicit `storage:` option in persist | XS |
| FH-02 | Hardcoded URL + raw fetch | Medium | `hooks/useWebSocket.ts:25` | Move `fetchWSTicket` to `api/wsTickets.ts` | S |
| FH-03 | useEffect cleanup miss | **High** | `hooks/useWebSocket.ts:70-107` | Guard `closedByCallerRef` after `await` | XS |
| FH-04 | useEffect cleanup miss | Info | `hooks/useStageLogs.ts:56-60` | No action needed | — |
| FH-05 | WS reconnect race | Medium | `hooks/useWebSocket.ts:62,111` | `onMessage` ref pattern (see P26-05-29) | S |
| FH-06 | WS reconnect race | Low-Med | `hooks/useWebSocket.ts:25,72-77` | Call `triggerSignIn()` on 401 in ticket fetch | XS |
| FH-07 | WS reconnect race | Low | `hooks/useWebSocket.ts:83-107` | Close existing socket before creating new | XS |

---

## "Will show up in Sentry next month" pick

**FH-03** — orphaned WebSocket on unmount-during-ticket-fetch.

This is a timing-sensitive race that hits whenever a user navigates away from a page with an active `useWebSocket` consumer (RunPage, any Kubernetes watch) while the initial or reconnect ticket fetch is in-flight. The scenario is common on flaky mobile connections where the ticket POST takes 200-800ms. The symptom will appear in Sentry as:

- `Warning: Can't perform a React state update on an unmounted component` (React 17) or a silent state update on an unmounted component (React 18 no-ops it but the connection leaks).
- Unexpected WebSocket `OPEN` events from connections no component is tracking — visible as repeated backend log entries for WS connections that immediately go idle.
- On repeated navigation (e.g., user pages through run history quickly), the leaked connections accumulate until the browser's per-origin WebSocket connection limit is reached (typically 256), at which point new connections are silently refused.

The fix is a single line: `if (closedByCallerRef.current) return;` inserted at line 72 of `useWebSocket.ts` immediately after the `await fetchWSTicket()` expression.
