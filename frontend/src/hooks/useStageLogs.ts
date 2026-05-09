import { useEffect, useRef, useState } from 'react';
import { useWebSocket } from './useWebSocket';
import { pipelineApi } from '../api/pipelines';

// MAX_LINES caps the rolling buffer. A pathological build can spew
// millions of lines; the on-disk capture is already capped at 1 MiB
// in the backend. The viewer only renders what the user can scroll
// through, so anything past 5000 is wasted memory.
const MAX_LINES = 5000;

export interface UseStageLogsOptions {
  pipelineId: string;
  runId: string;
  stageId: string;
  // enabled lets the parent gate the hook (e.g. when no stage is
  // selected). When false the hook holds zero connections / no buffer.
  enabled?: boolean;
}

export interface UseStageLogsResult {
  lines: string[];
  // backfillLoaded is true once the REST GetStageLogs has resolved.
  // Lets the UI distinguish "no logs yet" from "still fetching".
  backfillLoaded: boolean;
  connected: boolean;
  // truncated is true when the rolling buffer hit MAX_LINES and
  // started dropping the head. Surfaces a banner in the viewer.
  truncated: boolean;
}

/**
 * useStageLogs streams logs for a single (runId, stageId) into a
 * rolling line buffer.
 *
 * Backfill: on mount (or whenever the (pipelineId, runId, stageId)
 * triple changes), it fetches the on-disk capture via
 * GET /pipelines/:id/runs/:runId/logs/:stageId and seeds the buffer.
 *
 * Live tail: it subscribes to the WebSocket channel
 * /ws/runs/:runId/stages/:stageId/logs which the backend (B1) writes
 * one line per message. Reconnect / ticket refresh is owned by
 * useWebSocket so we don't duplicate that logic here.
 */
export function useStageLogs({
  pipelineId,
  runId,
  stageId,
  enabled = true,
}: UseStageLogsOptions): UseStageLogsResult {
  const [lines, setLines] = useState<string[]>([]);
  const [backfillLoaded, setBackfillLoaded] = useState(false);
  const [truncated, setTruncated] = useState(false);

  // Reset state whenever the target stage changes. Keeps the React
  // tree's mounted component but tears down its buffer cleanly.
  useEffect(() => {
    setLines([]);
    setBackfillLoaded(false);
    setTruncated(false);
  }, [pipelineId, runId, stageId, enabled]);

  // REST backfill. Cancel via abortRef so a fast triple-change
  // doesn't race with itself on fragile networks.
  const abortRef = useRef<AbortController | null>(null);
  useEffect(() => {
    if (!enabled || !pipelineId || !runId || !stageId) return;
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    pipelineApi
      .getStageLogs(pipelineId, runId, stageId)
      .then((res) => {
        if (ctrl.signal.aborted) return;
        const initial = (res.logs ?? '').split('\n');
        // split('') keeps the trailing empty string after a final
        // newline; drop it so the rendered list doesn't end with a
        // phantom blank line.
        if (initial.length > 0 && initial[initial.length - 1] === '') {
          initial.pop();
        }
        setLines(initial);
        setBackfillLoaded(true);
      })
      .catch(() => {
        if (!ctrl.signal.aborted) setBackfillLoaded(true);
      });
    return () => ctrl.abort();
  }, [enabled, pipelineId, runId, stageId]);

  // Live stream. Each message is a single line written by the
  // executor's lineWriter, including its trailing \n. Strip the \n
  // before storing so the renderer doesn't have to special-case it.
  const wsUrl = enabled && runId && stageId
    ? `/ws/runs/${encodeURIComponent(runId)}/stages/${encodeURIComponent(stageId)}/logs`
    : '';
  const { connected } = useWebSocket({
    url: wsUrl,
    autoConnect: enabled && !!wsUrl,
    onMessage: (data: unknown) => {
      // The backend tee broadcasts raw bytes; useWebSocket's onMessage
      // attempts JSON parse first and falls back to the raw payload.
      // Both forms reduce to a string that may include a trailing \n.
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
    },
  });

  return { lines, backfillLoaded, connected, truncated };
}
