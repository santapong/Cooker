// stageLogBuffer is the pure merge/dedup state machine behind
// useStageLogs. It is intentionally framework-free so the reconnect
// backfill logic (W6.2) can be unit-tested without a DOM: the hook is a
// thin shell that drives these reducers and mirrors `lines`/`truncated`
// into React state.
//
// Why this exists (docs/audits/2026-05-usestagelogs-reconnect.md, Q2):
// when the WebSocket drops and reconnects, lines emitted during the gap
// window must be recovered. Two sources cover the gap:
//
//   1. WS `?since=<lastSeq>` replay — the backend's bounded in-memory
//      logstore re-sends frames newer than lastSeq, IF it still retains
//      them (the ring can evict under load).
//   2. The REST capture (GET .../logs/:stageId) — the durable on-disk
//      blob, which survives ring eviction but carries NO per-line seq.
//
// The dedup contract that makes both safe to apply in any order:
//
//   - WS frames carry a 1-based monotonic `seq`; once the WS stream is
//     authoritative (after the first frame resets the REST seed, since
//     the stream replays from since=0), line index k holds seq k+1, so
//     `lines.length === lastSeq`.
//   - A reconnect REST merge therefore appends only the REST lines past
//     our current length (the evicted gap) and advances lastSeq to the
//     new length. Any WS replay frame for a line we just recovered is
//     then `<= lastSeq` and dropped. Whichever source lands first wins;
//     the other no-ops. No line is lost; none is duplicated.
//
// The one regime we deliberately do NOT index-merge is after the client
// rolling buffer has dropped its head (truncated): line index no longer
// aligns with seq, so REST can't be prefix-aligned to our tail. There the
// WS `?since=lastSeq` replay alone covers the recent gap and the user has
// already been shown the truncation banner.

// MAX_LINES caps the rolling buffer. A pathological build can spew
// millions of lines; the on-disk capture is already capped at 1 MiB in
// the backend. The viewer only renders what the user can scroll through,
// so anything past 5000 is wasted memory.
export const MAX_LINES = 5000;

// LogFrame is the wire shape stamped by the backend on every stage-log
// WS message (logstore.EncodeFrame).
export interface LogFrame {
  runId: string;
  stageId: string;
  seq: number;
  ts: string;
  line: string;
}

// ControlFrame is the drop signal the backend sends when it truncates the
// live stream (its in-memory buffer overflowed).
export interface ControlFrame {
  control: 'stream-truncated';
}

// StageLogState is the full mutable-by-copy buffer state. The hook keeps
// one of these in a ref and mirrors `lines` + `truncated` into React
// state for rendering.
export interface StageLogState {
  // Ordered, rendered lines (no trailing newline per line).
  lines: string[];
  // Highest WS seq folded in so far. 0 admits the first (seq=1) frame.
  lastSeq: number;
  // True after a REST backfill seeded `lines` and before the first WS
  // frame; the first WS frame discards the seed (the seq'd stream replays
  // from since=0 and is authoritative).
  seededFromRest: boolean;
  // True once the rolling buffer hit MAX_LINES and dropped its head.
  truncated: boolean;
}

export function initialStageLogState(): StageLogState {
  return { lines: [], lastSeq: 0, seededFromRest: false, truncated: false };
}

// isLogFrame returns true when the parsed object has the numeric seq and
// string line that mark a new-style log frame.
export function isLogFrame(v: unknown): v is LogFrame {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as Record<string, unknown>).seq === 'number' &&
    typeof (v as Record<string, unknown>).line === 'string'
  );
}

// isControlFrame returns true for the stream-truncated drop signal.
export function isControlFrame(v: unknown): v is ControlFrame {
  return (
    typeof v === 'object' &&
    v !== null &&
    (v as Record<string, unknown>).control === 'stream-truncated'
  );
}

// splitCapture turns the REST `{logs}` blob into an ordered line array,
// dropping the phantom trailing empty element a final newline leaves
// behind so the rendered list doesn't end with a blank line.
export function splitCapture(blob: string): string[] {
  const lines = (blob ?? '').split('\n');
  if (lines.length > 0 && lines[lines.length - 1] === '') {
    lines.pop();
  }
  return lines;
}

// capLines enforces MAX_LINES, returning the (possibly head-dropped)
// slice and whether truncation occurred on THIS step. The caller folds
// `didTruncate` into state.truncated (sticky once set).
function capLines(lines: string[]): { lines: string[]; didTruncate: boolean } {
  if (lines.length > MAX_LINES) {
    return { lines: lines.slice(lines.length - MAX_LINES), didTruncate: true };
  }
  return { lines, didTruncate: false };
}

// seedFromRest installs the initial REST backfill. It is a no-op once any
// WS frame has been seen (lastSeq > 0): the WS replay (since=0) has
// already seeded the authoritative buffer, so re-seeding here would
// duplicate the whole history alongside the live lines.
export function seedFromRest(state: StageLogState, blob: string): StageLogState {
  if (state.lastSeq > 0) {
    // WS already authoritative; keep what we have, just note the backfill
    // resolved. Nothing to merge.
    return state;
  }
  const seeded = splitCapture(blob);
  const { lines, didTruncate } = capLines(seeded);
  return {
    lines,
    lastSeq: 0,
    seededFromRest: true,
    truncated: state.truncated || didTruncate,
  };
}

// applyLogFrame folds one seq'd WS frame into the buffer. Frames at or
// below lastSeq are dropped (replay/live overlap + reconnect dedup). The
// first frame after a REST seed resets the buffer because the seq'd
// stream replays from since=0 and is authoritative.
export function applyLogFrame(state: StageLogState, frame: LogFrame): StageLogState {
  const { seq, line } = frame;
  if (seq <= state.lastSeq) {
    return state; // already rendered this line
  }
  const resetForReplay = state.seededFromRest;
  const base = resetForReplay ? [] : state.lines;
  const { lines, didTruncate } = capLines(base.concat(line));
  return {
    lines,
    lastSeq: seq,
    seededFromRest: false,
    // A reset for replay clears the prior REST-seed truncation flag; a
    // genuine overflow on this step (or a pre-existing one) keeps it set.
    truncated: (resetForReplay ? false : state.truncated) || didTruncate,
  };
}

// applyRawLine folds a pre-envelope raw string line (rollout back-compat,
// no seq) into the buffer. No dedup is possible without a seq, so this is
// append-only; it never resets the REST seed.
export function applyRawLine(state: StageLogState, raw: string): StageLogState {
  const line = raw.endsWith('\n') ? raw.slice(0, -1) : raw;
  const { lines, didTruncate } = capLines(state.lines.concat(line));
  return { ...state, lines, truncated: state.truncated || didTruncate };
}

// mergeReconnectBackfill recovers gap lines on reconnect (W6.2). It
// adopts only the REST capture lines BEYOND our current buffer length —
// the lines the bounded WS replay store may have evicted during the gap —
// and advances lastSeq to the new length so the subsequent WS
// `?since=lastSeq` replay dedupes those same lines instead of doubling
// them.
//
// Preconditions for a safe index-merge:
//   - The WS stream is authoritative (lastSeq > 0): only then does line
//     index align 1:1 with seq, making our buffer a true prefix of the
//     REST capture.
//   - We have not dropped our head (!truncated): a head-dropped buffer is
//     a suffix, not a prefix, so REST can't be index-aligned to it.
// When either fails we leave the buffer untouched and rely on the WS
// `?since=lastSeq` replay for the recent gap.
export function mergeReconnectBackfill(state: StageLogState, blob: string): StageLogState {
  if (state.lastSeq === 0) {
    // No WS frame yet: a normal seed path (or the initial backfill in
    // flight) owns this; don't second-guess it here.
    return state;
  }
  if (state.truncated) {
    // Head dropped — can't prefix-align. WS ?since replay covers the gap.
    return state;
  }
  const capture = splitCapture(blob);
  if (capture.length <= state.lines.length) {
    // REST has nothing we don't already hold. (It can legitimately be
    // SHORTER than our buffer: the on-disk capture lags the live ring.)
    return state;
  }
  const gap = capture.slice(state.lines.length);
  const { lines, didTruncate } = capLines(state.lines.concat(gap));
  // After the merge the buffer holds `capture.length` authoritative lines
  // (unless capped). lastSeq must advance to match so WS replay dedupes.
  // When capped we still advance to capture.length: seq is absolute and
  // the dropped head's seqs are below anything WS will resend with
  // ?since=lastSeq, so dedup stays correct.
  return {
    lines,
    lastSeq: capture.length,
    seededFromRest: false,
    truncated: state.truncated || didTruncate,
  };
}
