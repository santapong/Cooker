import { useEffect, useRef, useCallback, useState } from 'react';
import { getAccessToken } from '../auth/OIDCProvider';
import { API_ORIGIN, wsBase } from '../api/origin';

interface UseWebSocketOptions {
  url: string;
  onMessage?: (data: unknown) => void;
  autoConnect?: boolean;
  // reconnect controls the exponential backoff. Set enabled=false to
  // disable automatic reconnects entirely (e.g. in tests).
  reconnect?: {
    enabled?: boolean;
    initialDelayMs?: number;
    maxDelayMs?: number;
    maxAttempts?: number;
  };
  // getExtraParams is called just before each (re)connect and its
  // return value is appended to the WS URL as additional query params.
  // Use a ref-backed getter to avoid stale closures (the connect
  // callback reads it at call time, not at hook-mount time).
  getExtraParams?: () => Record<string, string>;
  // onReconnect fires each time a socket opens that is NOT the first
  // connection for this hook instance — i.e. after a drop+reconnect. The
  // `count` is the 1-based reconnect ordinal (first reconnect → 1). It is
  // the hook-level signal useStageLogs uses to re-issue its REST backfill
  // and recover lines lost during the gap window
  // (docs/audits/2026-05-usestagelogs-reconnect.md Q2). The initial open
  // does NOT call this (the mount-time backfill already covers it).
  onReconnect?: (count: number) => void;
}

// fetchWSTicket exchanges the user's bearer token for a single-use
// 60-second ticket. Browsers can't attach Authorization on a WS
// upgrade, so this is the auth handoff for /ws/* connections.
// Uses API_ORIGIN so that split-origin deployments (SPA on Vercel,
// API elsewhere) POST to the correct host.
async function fetchWSTicket(): Promise<string | null> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = getAccessToken();
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await fetch(`${API_ORIGIN}/api/v1/ws-tickets`, { method: 'POST', headers });
  if (!res.ok) return null;
  const body = (await res.json()) as { ticket?: string };
  return body.ticket ?? null;
}

const DEFAULT_INITIAL_DELAY = 500;
const DEFAULT_MAX_DELAY = 30_000;
const DEFAULT_MAX_ATTEMPTS = Infinity;

export function useWebSocket({
  url,
  onMessage,
  autoConnect = true,
  reconnect,
  getExtraParams,
  onReconnect,
}: UseWebSocketOptions) {
  const wsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);
  // reconnectCount is the number of times a socket has re-opened after a
  // drop (the initial open does not increment it). Exposed so callers can
  // watch it via useEffect as an alternative to the onReconnect callback.
  const [reconnectCount, setReconnectCount] = useState(0);

  const reconnectEnabled = reconnect?.enabled ?? true;
  const initialDelay = reconnect?.initialDelayMs ?? DEFAULT_INITIAL_DELAY;
  const maxDelay = reconnect?.maxDelayMs ?? DEFAULT_MAX_DELAY;
  const maxAttempts = reconnect?.maxAttempts ?? DEFAULT_MAX_ATTEMPTS;

  // P26-05-29: Stash onMessage in a ref so connect's identity doesn't
  // churn when the caller passes a fresh arrow on every render.  The
  // ws.onmessage handler reads onMessageRef.current at call time, so
  // it always sees the latest callback without being in connect's dep
  // array.  This prevents the useEffect([autoConnect, connect,
  // disconnect]) trigger from firing — and the resulting
  // disconnect+reconnect — just because the parent re-rendered.
  const onMessageRef = useRef(onMessage);
  useEffect(() => {
    onMessageRef.current = onMessage;
  });

  // Keep getExtraParams in a ref so the connect callback always reads
  // the latest getter without it being in connect's dep array.
  const getExtraParamsRef = useRef(getExtraParams);
  useEffect(() => {
    getExtraParamsRef.current = getExtraParams;
  });

  // Keep onReconnect in a ref for the same reason — the onopen handler
  // reads onReconnectRef.current at fire time so a fresh arrow on every
  // render doesn't churn connect's identity.
  const onReconnectRef = useRef(onReconnect);
  useEffect(() => {
    onReconnectRef.current = onReconnect;
  });

  // hasOpenedRef distinguishes the first successful open (no reconnect
  // signal — the mount-time backfill covers it) from every later one
  // (a drop+reconnect, which fires onReconnect / bumps reconnectCount).
  const hasOpenedRef = useRef(false);
  // reconnectCountRef mirrors the reconnectCount state so the onopen
  // handler can compute the next ordinal without reading state (which it
  // would close over stale).
  const reconnectCountRef = useRef(0);

  // Mutable refs so the connect callback's identity doesn't churn on
  // every attempt (which would re-trigger the autoConnect effect).
  const attemptRef = useRef(0);
  const timerRef = useRef<number | null>(null);
  const closedByCallerRef = useRef(false);

  const clearTimer = () => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };

  const connect = useCallback(async () => {
    if (!url) {
      // Defence-in-depth: do not attempt a connect with an empty URL.
      // Today the autoConnect gate in useStageLogs prevents this from
      // firing; the guard is here so future callers that drop the
      // gate don't silently connect to the wrong endpoint.
      return;
    }
    // Supersede any existing socket before opening a new one. A caller
    // can invoke connect() while a socket is still live (e.g. useStageLogs
    // calls connect() on a `stream-truncated` control frame). Without this
    // the old socket stays open and, because we reset closedByCallerRef
    // below, its onclose would fire scheduleReconnect() — a reconnect
    // storm plus a leaked socket. Detach the old socket's handlers so its
    // onclose is a no-op, then close it. We can't reuse closedByCallerRef
    // for this (that flag also guards unmount-during-ticket-fetch and
    // gates scheduleReconnect for the *new* attempt), so we neutralise the
    // superseded socket per-instance.
    const stale = wsRef.current;
    if (stale && stale.readyState !== WebSocket.CLOSED) {
      stale.onopen = null;
      stale.onclose = null;
      stale.onerror = null;
      stale.onmessage = null;
      stale.close();
    }
    wsRef.current = null;
    closedByCallerRef.current = false;
    const ticket = await fetchWSTicket();
    if (closedByCallerRef.current) return; // FH-03: guard against unmount during ticket fetch
    if (!ticket) {
      // Auth failed — schedule a retry with backoff so an expired
      // token that gets refreshed in the background eventually
      // recovers without a page reload.
      scheduleReconnect();
      return;
    }
    // Build extra params from the caller's getter (read at connect
    // time so reconnects always see the latest values, e.g. lastSeq).
    const extraParams = getExtraParamsRef.current?.() ?? {};
    const extraParamStr = Object.entries(extraParams)
      .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
      .join('&');
    const base = extraParamStr ? `${url}?${extraParamStr}` : url;
    const sep = base.includes('?') ? '&' : '?';
    // wsBase() returns the correct wss:// or ws:// authority — either
    // from VITE_API_BASE_URL (split-origin) or window.location (same-origin).
    const wsUrl = `${wsBase()}${base}${sep}ticket=${encodeURIComponent(ticket)}`;

    const ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      attemptRef.current = 0;
      setConnected(true);
      // Fire the reconnect signal on every open AFTER the first. The
      // first open is the initial subscription, already covered by the
      // mount-time REST backfill; later opens follow a drop and need a
      // gap-recovery backfill.
      if (hasOpenedRef.current) {
        const next = reconnectCountRef.current + 1;
        reconnectCountRef.current = next;
        setReconnectCount(next);
        onReconnectRef.current?.(next);
      } else {
        hasOpenedRef.current = true;
      }
    };
    ws.onclose = () => {
      setConnected(false);
      if (!closedByCallerRef.current) {
        scheduleReconnect();
      }
    };
    ws.onerror = () => {
      // onclose fires after onerror; the close handler triggers reconnect.
    };
    ws.onmessage = (event) => {
      // Read from the ref so we always call the latest onMessage
      // without onMessage being in this useCallback's dep array
      // (P26-05-29).
      try {
        const data = JSON.parse(event.data);
        onMessageRef.current?.(data);
      } catch {
        onMessageRef.current?.(event.data);
      }
    };

    wsRef.current = ws;
    // The connect call below depends on scheduleReconnect, which itself
    // depends on connect — declared via refs to avoid the cycle.
    // onMessage is intentionally omitted: it lives in onMessageRef and
    // is updated synchronously before every render via the useEffect
    // above (P26-05-29).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url]);

  const scheduleReconnect = useCallback(() => {
    if (!reconnectEnabled) return;
    if (closedByCallerRef.current) return;
    if (attemptRef.current >= maxAttempts) return;
    const attempt = attemptRef.current;
    attemptRef.current = attempt + 1;
    const delay = Math.min(initialDelay * 2 ** attempt, maxDelay);
    clearTimer();
    timerRef.current = window.setTimeout(() => {
      void connect();
    }, delay);
  }, [reconnectEnabled, maxAttempts, initialDelay, maxDelay, connect]);

  const disconnect = useCallback(() => {
    closedByCallerRef.current = true;
    clearTimer();
    attemptRef.current = 0;
    wsRef.current?.close();
    wsRef.current = null;
    setConnected(false);
  }, []);

  const send = useCallback((data: unknown) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(data));
    }
  }, []);

  useEffect(() => {
    // `connect` identity changes iff `url` changes (its only dep), so a
    // new URL (e.g. useStageLogs switching stages) starts a fresh
    // subscription: clear the "has opened" / reconnect bookkeeping so the
    // first open on the new URL is treated as an initial connect, not a
    // reconnect. Within a single URL the refs persist across the backoff
    // reconnect loop, which is what lets us distinguish opens 2..N.
    hasOpenedRef.current = false;
    reconnectCountRef.current = 0;
    setReconnectCount(0);
    if (autoConnect) {
      void connect();
    }
    return () => disconnect();
  }, [autoConnect, connect, disconnect]);

  return { connected, connect, disconnect, send, reconnectCount };
}
