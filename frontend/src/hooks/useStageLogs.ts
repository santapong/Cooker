import { useEffect, useRef, useState } from 'react';
import { useWebSocket } from './useWebSocket';
import { pipelineApi } from '../api/pipelines';

// MAX_LINES caps the rolling buffer. A pathological build can spew
// millions of lines; the on-disk capture is already capped at 1 MiB
// in the backend. The viewer only renders what the user can scroll
// through, so anything past 5000 is wasted memory.
const MAX_LINES = 5000;

// LogFrame is the new wire shape stamped by the backend on every
// stage-log WS message.
interface LogFrame {
  runId: string;
  stageId: string;
  seq: number;
  ts: string;
  line: string;
}

// ControlFrame is the drop signal the backend sends when it truncates
// the live stream (e.g. the in-memory buffer overflowed).
interface ControlFrame {
  control: 'stream-truncated';
}

// isLogFrame returns true when the parsed object has the numeric seq
// and string line that mark a new-style log frame.
function isLogFrame(v: unknown): v is LogFrame {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as Record<string, unknown>).seq === 'number' &&
    typeof (v as Record<string, unknown>).line === 'string'
  );
}

// isControlFrame returns true for the stream-truncated drop signal.
function isControlFrame(v: unknown): v is ControlFrame {
  return (
    typeof v === 'object' &&
    v !== null &&
    (v as Record<string, unknown>).control === 'stream-truncated'
  );
}

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
 * /ws/runs/:runId/stages/:stageId/logs which the backend (B1) writes
 * one line per message. Reconnect / ticket refresh is owned by
 * useWebSocket so we don't duplicate that logic here.
 *
 * Seq-envelope handling (log replay support):
 * - Each WS frame is now JSON with a numeric `seq` and string `line`.
 * - `lastSeq` tracks the highest seq seen; frames with seq <= lastSeq
 *   are silently dropped (replay + live overlap dedup).
 * - On (re)connect the current `lastSeq` is passed as `?since=<seq>`
 *   so the backend replays only missing frames.
 * - On the first seq'd frame after a REST backfill, the buffer is
 *   reset (WS stream is authoritative and replays from since=0).
 * - On `{"control":"stream-truncated"}` the streamTruncated flag is
 *   set and a reconnect is triggered with the current lastSeq so the
 *   backend refills from its replay store.
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
  const [streamTruncated, setStreamTruncated] = useState(false);

  // lastSeqRef holds the highest seq seen so far. A ref (not state)
  // so the reconnect closure in useWebSocket always reads the latest
  // value without triggering a re-render or stale-closure problems.
  const lastSeqRef = useRef<number>(0);

  // seededFromRestRef is true after the REST backfill populates the
  // buffer and before any seq'd WS frame has arrived. On the first
  // seq'd frame we discard the REST data (the seq'd stream replays
  // from since=0 and is authoritative).
  const seededFromRestRef = useRef<boolean>(false);

  // Reset state whenever the target stage changes. Keeps the React
  // tree's mounted component but tears down its buffer cleanly.
  useEffect(() => {
    setLines([]);
    setBackfillLoaded(false);
    setTruncated(false);
    setStreamTruncated(false);
    lastSeqRef.current = 0;
    seededFromRestRef.current = false;
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
        // If a seq'd WS frame already arrived, the WS replay (since=0)
        // has already seeded the authoritative buffer — discard the REST
        // result entirely. Seeding here would duplicate the full history
        // alongside the WS lines (the WS frames won't reset the buffer
        // because seededFromRestRef would never get set in this ordering).
        if (lastSeqRef.current > 0) {
          setBackfillLoaded(true);
          return;
        }
        const initial = (res.logs ?? '').split('\n');
        // split('') keeps the trailing empty string after a final
        // newline; drop it so the rendered list doesn't end with a
        // phantom blank line.
        if (initial.length > 0 && initial[initial.length - 1] === '') {
          initial.pop();
        }
        setLines(initial);
        setBackfillLoaded(true);
        // Mark the buffer as REST-seeded so the first seq'd WS frame
        // knows to reset it.
        seededFromRestRef.current = true;
      })
      .catch(() => {
        if (!ctrl.signal.aborted) setBackfillLoaded(true);
      });
    return () => ctrl.abort();
  }, [enabled, pipelineId, runId, stageId]);

  // Live stream. Each message is now a JSON envelope with a seq number.
  // The since param is read fresh on each (re)connect via getExtraParams.
  const wsUrl = enabled && runId && stageId
    ? `/ws/runs/${encodeURIComponent(runId)}/stages/${encodeURIComponent(stageId)}/logs`
    : '';

  const { connected, connect } = useWebSocket({
    url: wsUrl,
    autoConnect: enabled && !!wsUrl,
    // getExtraParams is called just before each WS open, reading the
    // latest lastSeqRef so reconnects resume from the last seen line.
    getExtraParams: wsUrl
      ? () => ({ since: String(lastSeqRef.current) })
      : undefined,
    onMessage: (data: unknown) => {
      // --- New envelope: control frame (stream-truncated) ---
      if (isControlFrame(data)) {
        setStreamTruncated(true);
        // Trigger an immediate reconnect with since=lastSeq so the
        // backend can refill from its replay store.
        void connect();
        return;
      }

      // --- New envelope: seq'd log frame ---
      if (isLogFrame(data)) {
        const { seq, line } = data;

        // Dedupe: ignore any frame we've already rendered. Backend `seq`
        // is 1-based (the first frame has seq=1), so the initial
        // lastSeqRef value of 0 correctly admits the very first frame.
        if (seq <= lastSeqRef.current) return;

        // First seq'd frame after REST backfill: the seq'd stream is
        // authoritative and replays from since=0, so discard the
        // REST-seeded lines. Capture the decision synchronously and flip
        // the ref now, then fold the reset INTO the functional setLines
        // update. If two frames arrive in the same tick, only the first
        // sees seededFromRestRef=true; doing the reset inside the updater
        // means the second frame appends to the already-reset buffer
        // rather than racing a separate setLines([]) against stale state.
        const resetForReplay = seededFromRestRef.current;
        if (resetForReplay) {
          seededFromRestRef.current = false;
          setTruncated(false);
        }

        lastSeqRef.current = seq;

        setLines((prev) => {
          const base = resetForReplay ? [] : prev;
          const next = base.concat(line);
          if (next.length > MAX_LINES) {
            setTruncated(true);
            return next.slice(next.length - MAX_LINES);
          }
          return next;
        });
        return;
      }

      // --- Back-compat fallback: raw string (pre-envelope rollout) ---
      // The backend may still send raw lines during the rollout window.
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

  return { lines, backfillLoaded, connected, truncated, streamTruncated };
}
