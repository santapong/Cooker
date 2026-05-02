import { useEffect, useRef, useCallback, useState } from 'react';
import { getAccessToken } from '../auth/OIDCProvider';

interface UseWebSocketOptions {
  url: string;
  onMessage?: (data: unknown) => void;
  autoConnect?: boolean;
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

export function useWebSocket({ url, onMessage, autoConnect = true }: UseWebSocketOptions) {
  const wsRef = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);

  const connect = useCallback(async () => {
    const ticket = await fetchWSTicket();
    if (!ticket) {
      // Auth failed — leave connected=false; caller can retry.
      return;
    }
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const sep = url.includes('?') ? '&' : '?';
    const wsUrl = `${protocol}//${window.location.host}${url}${sep}ticket=${encodeURIComponent(ticket)}`;

    const ws = new WebSocket(wsUrl);

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);
    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        onMessage?.(data);
      } catch {
        onMessage?.(event.data);
      }
    };

    wsRef.current = ws;
  }, [url, onMessage]);

  const disconnect = useCallback(() => {
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
