import { useCallback, useEffect, useRef, useState } from 'react';
import { useWebSocket } from './useWebSocket';
import { pipelineApi } from '../api/pipelines';
import {
  initialStageLogState,
  isControlFrame,
  isLogFrame,
  applyLogFrame,
  applyRawLine,
  seedFromRest,
  mergeReconnectBackfill,
  type StageLogState,
} from './stageLogBuffer';

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
  // streamTruncated is true when the backend sent a stream-truncated
  // control frame (in-memory store overflow). Distinct from the
  // client-side rolling-buffer truncation above. The UI should
  // surface a "live logs truncated — reload" banner.
  streamTruncated: boolean;
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
 * /ws/runs/:runId/stages/:stageId/logs which the backend writes one
 * line per message. Reconnect / ticket refresh is owned by useWebSocket
 * so we don't duplicate that logic here.
 *
 * Reconnect backfill (W6.2, docs/audits/2026-05-usestagelogs-reconnect.md):
 * on every WS reconnect, useWebSocket calls our onReconnect handler. We
 * re-issue the REST capture and merge it via mergeReconnectBackfill,
 * recovering any lines the bounded WS replay store evicted during the
 * gap window. The merge advances lastSeq so the WS `?since=<lastSeq>`
 * replay dedupes the same lines instead of doubling them — see
 * stageLogBuffer.ts for the full dedup contract.
 *
 * All buffer mutation lives in the pure stageLogBuffer module; this hook
 * owns transport (WS + REST), the abortable fetch, and mirroring the
 * buffer into React state for rendering.
 */
export function useStageLogs({
  pipelineId,
  runId,
  stageId,
  enabled = true,
}: UseStageLogsOptions): UseStageLogsResult {
  // bufferRef holds the authoritative buffer state; React state mirrors
  // the renderable slices. A ref (not state) so the WS onMessage closure
  // and the reconnect handler always read/write the latest buffer without
  // stale-closure problems and without each frame forcing a dep-array
  // churn.
  const bufferRef = useRef<StageLogState>(initialStageLogState());

  const [lines, setLines] = useState<string[]>([]);
  const [truncated, setTruncated] = useState(false);
  const [backfillLoaded, setBackfillLoaded] = useState(false);
  const [streamTruncated, setStreamTruncated] = useState(false);

  // commit mirrors the current buffer into the rendered React state. Both
  // setters take new references each commit; React bails on no-op updates
  // by identity so a frame that didn't change `truncated` is cheap.
  const commit = useCallback(() => {
    setLines(bufferRef.current.lines);
    setTruncated(bufferRef.current.truncated);
  }, []);

  // lastSeqRef exposes the buffer's seq cursor to the WS getExtraParams
  // getter (read fresh on each (re)connect for `?since=<seq>`). Kept in
  // sync with the buffer on every mutation.
  const lastSeqRef = useRef<number>(0);
  const syncSeq = useCallback(() => {
    lastSeqRef.current = bufferRef.current.lastSeq;
  }, []);

  // Reset everything whenever the target stage changes. Keeps the mounted
  // component but tears down its buffer cleanly.
  useEffect(() => {
    bufferRef.current = initialStageLogState();
    lastSeqRef.current = 0;
    setLines([]);
    setTruncated(false);
    setBackfillLoaded(false);
    setStreamTruncated(false);
  }, [pipelineId, runId, stageId, enabled]);

  // fetchBackfill issues the REST capture and applies it via `apply`
  // (seedFromRest on mount, mergeReconnectBackfill on reconnect). The
  // abortRef guards against a fast triple-change / overlapping reconnect
  // racing itself on fragile networks.
  const abortRef = useRef<AbortController | null>(null);
  const fetchBackfill = useCallback(
    (apply: (state: StageLogState, blob: string) => StageLogState) => {
      if (!enabled || !pipelineId || !runId || !stageId) return;
      abortRef.current?.abort();
      const ctrl = new AbortController();
      abortRef.current = ctrl;
      pipelineApi
        .getStageLogs(pipelineId, runId, stageId)
        .then((res) => {
          if (ctrl.signal.aborted) return;
          bufferRef.current = apply(bufferRef.current, res.logs ?? '');
          syncSeq();
          setBackfillLoaded(true);
          commit();
        })
        .catch(() => {
          if (!ctrl.signal.aborted) setBackfillLoaded(true);
        });
    },
    [enabled, pipelineId, runId, stageId, commit, syncSeq],
  );

  // Mount-time backfill: seed the buffer from the on-disk capture.
  useEffect(() => {
    fetchBackfill(seedFromRest);
    return () => abortRef.current?.abort();
  }, [fetchBackfill]);

  const wsUrl =
    enabled && runId && stageId
      ? `/ws/runs/${encodeURIComponent(runId)}/stages/${encodeURIComponent(stageId)}/logs`
      : '';

  const { connected, connect } = useWebSocket({
    url: wsUrl,
    autoConnect: enabled && !!wsUrl,
    // getExtraParams is called just before each WS open, reading the
    // latest lastSeqRef so reconnects resume from the last seen line.
    getExtraParams: wsUrl ? () => ({ since: String(lastSeqRef.current) }) : undefined,
    // onReconnect fires on every reconnect (not the initial open). Re-fetch
    // the REST capture and merge the gap window the WS replay store may
    // have dropped (W6.2).
    onReconnect: wsUrl ? () => fetchBackfill(mergeReconnectBackfill) : undefined,
    onMessage: (data: unknown) => {
      // Control frame: the backend dropped us from the live stream. Set
      // the banner flag and reconnect with since=lastSeq so the backend
      // refills from its replay store.
      if (isControlFrame(data)) {
        setStreamTruncated(true);
        void connect();
        return;
      }

      // Seq'd log frame: the common path. All dedup / reset / cap logic is
      // in the pure reducer.
      if (isLogFrame(data)) {
        bufferRef.current = applyLogFrame(bufferRef.current, data);
        syncSeq();
        commit();
        return;
      }

      // Back-compat fallback: raw string line (pre-envelope rollout).
      const raw = typeof data === 'string' ? data : JSON.stringify(data);
      bufferRef.current = applyRawLine(bufferRef.current, raw);
      commit();
    },
  });

  return { lines, backfillLoaded, connected, truncated, streamTruncated };
}
