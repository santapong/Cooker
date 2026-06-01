import { useEffect, useRef, useCallback, useState } from 'react';
import { getAccessToken } from '../auth/OIDCProvider';

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
}

// fetchWSTicket exchanges the user's bearer token for a single-use
// 60-second ticket. Browsers can't attach Authorization on a WS
// upgrade, so this is the auth handoff for /ws/* connections.
async function fetchWSTicket(): Promise<string | null> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = getAccessToken();
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await fetch('/api/v1/ws-tickets', { method: 'POST', headers });
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
}: UseWebSocketOptions) {
  const wsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);

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
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    // Build extra params from the caller's getter (read at connect
    // time so reconnects always see the latest values, e.g. lastSeq).
    const extraParams = getExtraParamsRef.current?.() ?? {};
    const extraParamStr = Object.entries(extraParams)
      .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
      .join('&');
    const base = extraParamStr ? `${url}?${extraParamStr}` : url;
    const sep = base.includes('?') ? '&' : '?';
    const wsUrl = `${protocol}//${window.location.host}${base}${sep}ticket=${encodeURIComponent(ticket)}`;

    const ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      attemptRef.current = 0;
      setConnected(true);
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
    if (autoConnect) {
      void connect();
    }
    return () => disconnect();
  }, [autoConnect, connect, disconnect]);

  return { connected, connect, disconnect, send };
}
