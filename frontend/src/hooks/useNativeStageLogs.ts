import { useCallback, useEffect, useState } from 'react';
import { nativeRuntimeLogsPath } from '../api/nativeRuntime';
import type { NativeRuntimeSubject } from '../types/nativeRuntime';
import { useWebSocket } from './useWebSocket';

/** Mount with a subject key: pending tickets from an old target must unmount too. */
export function useNativeStageLogs(subject: NativeRuntimeSubject) {
  const [lines, setLines] = useState<string[]>([]);
  const [truncated, setTruncated] = useState(false);
  const { pipelineId, runId, stageId } = subject;
  useEffect(() => { setLines([]); setTruncated(false); }, [pipelineId, runId, stageId]);
  const onMessage = useCallback((data: unknown) => {
    if (typeof data === 'object' && data !== null && 'control' in data && data.control === 'stream-truncated') { setTruncated(true); return; }
    const raw = typeof data === 'string' ? data : JSON.stringify(data);
    const line = raw.endsWith('\n') ? raw.slice(0, -1) : raw;
    setLines((previous) => {
      const next = previous.concat(line);
      if (next.length > 5000) { setTruncated(true); return next.slice(-5000); }
      return next;
    });
  }, []);
  const { connected } = useWebSocket({ url: nativeRuntimeLogsPath(subject), autoConnect: true, onMessage });
  return { lines, truncated, connected };
}
