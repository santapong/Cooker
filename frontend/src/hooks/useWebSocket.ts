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
}: UseWebSocketOptions) {
  const wsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);

  const reconnectEnabled = reconnect?.enabled ?? true;
  const initialDelay = reconnect?.initialDelayMs ?? DEFAULT_INITIAL_DELAY;
  const maxDelay = reconnect?.maxDelayMs ?? DEFAULT_MAX_DELAY;
  const maxAttempts = reconnect?.maxAttempts ?? DEFAULT_MAX_ATTEMPTS;

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
    closedByCallerRef.current = false;
    const ticket = await fetchWSTicket();
    if (!ticket) {
      // Auth failed — schedule a retry with backoff so an expired
      // token that gets refreshed in the background eventually
      // recovers without a page reload.
      scheduleReconnect();
      return;
    }
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const sep = url.includes('?') ? '&' : '?';
    const wsUrl = `${protocol}//${window.location.host}${url}${sep}ticket=${encodeURIComponent(ticket)}`;

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
      try {
        const data = JSON.parse(event.data);
        onMessage?.(data);
      } catch {
        onMessage?.(event.data);
      }
    };

    wsRef.current = ws;
    // The connect call below depends on scheduleReconnect, which itself
    // depends on connect — declared via refs to avoid the cycle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, onMessage]);

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
