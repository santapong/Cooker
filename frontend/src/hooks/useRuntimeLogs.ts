import { useCallback, useEffect, useState } from 'react';
import { useWebSocket } from './useWebSocket';
import { runtimeApi } from '../api/pipelines';

const MAX_LINES = 5000;

export interface UseRuntimeLogsOptions {
  appId: string;
  serviceId: string;
  // enabled lets the parent gate the hook (e.g. when no node is
  // selected). When false the hook holds zero connections / no buffer.
  enabled?: boolean;
}

export interface UseRuntimeLogsResult {
  lines: string[];
  connected: boolean;
  truncated: boolean;
}

/**
 * useRuntimeLogs tails the live container/pod logs of a deployed
 * compose service over /ws/runtime/:appId/:serviceId/logs. Unlike
 * useStageLogs there is no REST backfill — these are live runtime
 * logs, streamed line-by-line by the backend RuntimeService. Reconnect
 * / ticket refresh is owned by useWebSocket.
 */
export function useRuntimeLogs({
  appId,
  serviceId,
  enabled = true,
}: UseRuntimeLogsOptions): UseRuntimeLogsResult {
  const [lines, setLines] = useState<string[]>([]);
  const [truncated, setTruncated] = useState(false);

  // Reset the buffer whenever the target service changes.
  useEffect(() => {
    setLines([]);
    setTruncated(false);
  }, [appId, serviceId, enabled]);

  // Wrap onMessage in useCallback with an empty dep array — setLines and
  // setTruncated are stable dispatcher references (React guarantees this)
  // so no deps are needed. An inline arrow on every render would cause
  // useWebSocket to see a new onMessage on each render, triggering
  // unnecessary reconnects (FE-M useRuntimeLogs).
  const onMessage = useCallback((data: unknown) => {
    const raw = typeof data === 'string' ? data : JSON.stringify(data);
    const line = raw.endsWith('\n') ? raw.slice(0, -1) : raw;
    setLines((prev) => {
      const next = prev.concat(line);
      if (next.length > MAX_LINES) {
        setTruncated(true);
        return next.slice(next.length - MAX_LINES);
      }
      return next;
    });
  }, []);

  const wsUrl = enabled && appId && serviceId ? runtimeApi.logsPath(appId, serviceId) : '';
  const { connected } = useWebSocket({
    url: wsUrl,
    autoConnect: enabled && !!wsUrl,
    onMessage,
  });

  return { lines, connected, truncated };
}
