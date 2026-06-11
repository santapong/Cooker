import { describe, it, expect } from 'vitest';
import {
  MAX_LINES,
  initialStageLogState,
  isControlFrame,
  isLogFrame,
  splitCapture,
  seedFromRest,
  applyLogFrame,
  applyRawLine,
  mergeReconnectBackfill,
  type LogFrame,
  type StageLogState,
} from './stageLogBuffer';

// frame builds a seq'd WS log frame the way logstore.EncodeFrame does.
function frame(seq: number, line: string): LogFrame {
  return { runId: 'r1', stageId: 's1', seq, ts: '2026-06-11T00:00:00.000Z', line };
}

// applyFrames folds a list of WS frames in order (as the live stream /
// replay would deliver them).
function applyFrames(state: StageLogState, frames: LogFrame[]): StageLogState {
  return frames.reduce((s, f) => applyLogFrame(s, f), state);
}

// captureOf renders the backend's `{logs}` blob for the first n lines of
// `all` (the on-disk capture is newline-joined WITH a trailing newline).
function captureOf(all: string[], n: number): string {
  return all.slice(0, n).map((l) => l).join('\n') + (n > 0 ? '\n' : '');
}

describe('frame type guards', () => {
  it('isLogFrame accepts a seq+line object and rejects others', () => {
    expect(isLogFrame(frame(1, 'a'))).toBe(true);
    expect(isLogFrame({ control: 'stream-truncated' })).toBe(false);
    expect(isLogFrame('raw line')).toBe(false);
    expect(isLogFrame(null)).toBe(false);
  });

  it('isControlFrame accepts the truncation signal only', () => {
    expect(isControlFrame({ control: 'stream-truncated' })).toBe(true);
    expect(isControlFrame(frame(1, 'a'))).toBe(false);
    expect(isControlFrame(null)).toBe(false);
  });
});

describe('splitCapture', () => {
  it('drops the phantom trailing empty line from a final newline', () => {
    expect(splitCapture('a\nb\nc\n')).toEqual(['a', 'b', 'c']);
  });
  it('keeps an intentional blank middle line', () => {
    expect(splitCapture('a\n\nc\n')).toEqual(['a', '', 'c']);
  });
  it('handles empty / undefined blobs', () => {
    expect(splitCapture('')).toEqual([]);
    expect(splitCapture(undefined as unknown as string)).toEqual([]);
  });
});

describe('seedFromRest + first WS frame', () => {
  it('seeds from the REST capture, then the first WS frame replaces it (since=0 replay is authoritative)', () => {
    let s = initialStageLogState();
    s = seedFromRest(s, 'one\ntwo\n');
    expect(s.lines).toEqual(['one', 'two']);
    expect(s.seededFromRest).toBe(true);
    expect(s.lastSeq).toBe(0);

    // The WS replays from seq=1; the first frame discards the REST seed.
    s = applyLogFrame(s, frame(1, 'one'));
    expect(s.lines).toEqual(['one']);
    expect(s.seededFromRest).toBe(false);
    expect(s.lastSeq).toBe(1);

    s = applyLogFrame(s, frame(2, 'two'));
    s = applyLogFrame(s, frame(3, 'three'));
    expect(s.lines).toEqual(['one', 'two', 'three']);
    expect(s.lastSeq).toBe(3);
    // Authoritative invariant: line count == lastSeq.
    expect(s.lines.length).toBe(s.lastSeq);
  });

  it('ignores a late REST seed once WS frames have arrived (no duplication)', () => {
    let s = initialStageLogState();
    s = applyFrames(s, [frame(1, 'a'), frame(2, 'b')]);
    // REST resolves late; since lastSeq>0 it must NOT re-seed.
    s = seedFromRest(s, 'a\nb\n');
    expect(s.lines).toEqual(['a', 'b']);
    expect(s.lastSeq).toBe(2);
  });
});

describe('applyLogFrame dedup', () => {
  it('drops frames at or below lastSeq (replay/live overlap)', () => {
    let s = initialStageLogState();
    s = applyFrames(s, [frame(1, 'a'), frame(2, 'b'), frame(3, 'c')]);
    // A replay re-delivers 1..3; all are <= lastSeq and must be dropped.
    s = applyFrames(s, [frame(1, 'a'), frame(2, 'b'), frame(3, 'c')]);
    expect(s.lines).toEqual(['a', 'b', 'c']);
    expect(s.lastSeq).toBe(3);
  });

  it('applyRawLine appends pre-envelope raw lines and strips a trailing newline', () => {
    let s = initialStageLogState();
    s = applyRawLine(s, 'raw1\n');
    s = applyRawLine(s, 'raw2');
    expect(s.lines).toEqual(['raw1', 'raw2']);
  });
});

describe('reconnect — WS ?since replay path (ring still retains the gap)', () => {
  it('recovers all gap lines with NO loss and NO duplicates', () => {
    const all = ['l1', 'l2', 'l3', 'l4', 'l5'];
    let s = initialStageLogState();
    // Pre-drop: we saw l1..l2 live.
    s = applyFrames(s, [frame(1, 'l1'), frame(2, 'l2')]);
    expect(s.lastSeq).toBe(2);

    // --- WS drops here. l3..l5 are emitted during the gap. ---

    // Reconnect: useStageLogs re-fetches REST first. In THIS path the
    // backend ring still holds l3..l5, so REST is also fully caught up.
    // mergeReconnectBackfill appends the gap (l3..l5) and advances lastSeq.
    s = mergeReconnectBackfill(s, captureOf(all, 5));
    expect(s.lines).toEqual(all);
    expect(s.lastSeq).toBe(5);

    // Then the WS `?since=2` replay re-delivers l3..l5. Every one is now
    // <= lastSeq(5) and must be deduped — this is the core W6.2 guarantee.
    s = applyFrames(s, [frame(3, 'l3'), frame(4, 'l4'), frame(5, 'l5')]);
    expect(s.lines).toEqual(all);
    expect(s.lastSeq).toBe(5);
    // No duplicates: rendered length equals the distinct line count.
    expect(s.lines.length).toBe(new Set(s.lines.map((l, i) => `${i}:${l}`)).size);
    expect(s.lines.length).toBe(5);
  });

  it('is order-independent: WS replay landing BEFORE the REST merge also dedupes', () => {
    const all = ['l1', 'l2', 'l3', 'l4', 'l5'];
    let s = initialStageLogState();
    s = applyFrames(s, [frame(1, 'l1'), frame(2, 'l2')]);

    // Race: WS `?since=2` replay arrives first on reconnect.
    s = applyFrames(s, [frame(3, 'l3'), frame(4, 'l4'), frame(5, 'l5')]);
    expect(s.lines).toEqual(all);
    expect(s.lastSeq).toBe(5);

    // The REST merge lands second; it has nothing beyond our length → no-op.
    s = mergeReconnectBackfill(s, captureOf(all, 5));
    expect(s.lines).toEqual(all);
    expect(s.lastSeq).toBe(5);
  });
});

describe('reconnect — REST recovery path (ring EVICTED the gap)', () => {
  it('recovers evicted gap lines from REST when the WS replay cannot', () => {
    const all = ['l1', 'l2', 'l3', 'l4', 'l5'];
    let s = initialStageLogState();
    s = applyFrames(s, [frame(1, 'l1'), frame(2, 'l2')]);

    // --- WS drops. l3..l5 emitted during a LONG gap; the bounded ring
    // evicts them, so the WS `?since=2` replay on reconnect returns
    // NOTHING for l3..l5. Only the durable REST capture still has them. ---

    s = mergeReconnectBackfill(s, captureOf(all, 5));
    // The gap is recovered purely from REST.
    expect(s.lines).toEqual(all);
    expect(s.lastSeq).toBe(5);

    // WS resumes with NEW lines l6..l7 (seq 6,7 > lastSeq) — appended.
    s = applyFrames(s, [frame(6, 'l6'), frame(7, 'l7')]);
    expect(s.lines).toEqual([...all, 'l6', 'l7']);
    expect(s.lastSeq).toBe(7);
    // Still no duplicates anywhere.
    expect(new Set(s.lines).size).toBe(s.lines.length);
  });

  it('handles a REST capture that LAGS the live ring (shorter than buffer): no-op, no truncation of live lines', () => {
    const all = ['l1', 'l2', 'l3', 'l4'];
    let s = initialStageLogState();
    s = applyFrames(s, all.map((l, i) => frame(i + 1, l)));
    expect(s.lastSeq).toBe(4);

    // On-disk capture only flushed l1..l2 so far (lags the live stream).
    s = mergeReconnectBackfill(s, captureOf(all, 2));
    // Must NOT drop l3..l4 just because REST is behind.
    expect(s.lines).toEqual(all);
    expect(s.lastSeq).toBe(4);
  });
});

describe('reconnect guards', () => {
  it('mergeReconnectBackfill is a no-op before any WS frame (lastSeq=0)', () => {
    let s = initialStageLogState();
    s = seedFromRest(s, 'a\nb\n');
    const before = s;
    s = mergeReconnectBackfill(s, 'a\nb\nc\n');
    // Owned by the seed path; merge must not touch a pre-WS buffer.
    expect(s).toEqual(before);
    expect(s.lastSeq).toBe(0);
  });

  it('mergeReconnectBackfill is a no-op once the rolling buffer has dropped its head', () => {
    // Drive the buffer past MAX_LINES so truncated flips and the head is
    // dropped — then the index-prefix assumption no longer holds.
    let s = initialStageLogState();
    const frames: LogFrame[] = [];
    for (let i = 1; i <= MAX_LINES + 10; i++) frames.push(frame(i, `l${i}`));
    s = applyFrames(s, frames);
    expect(s.truncated).toBe(true);
    expect(s.lines.length).toBe(MAX_LINES);
    const linesBefore = s.lines;
    const seqBefore = s.lastSeq;

    // A reconnect REST merge must bail (can't prefix-align a head-dropped
    // buffer); the WS ?since replay covers the recent gap instead.
    s = mergeReconnectBackfill(s, captureOf(frames.map((f) => f.line), MAX_LINES + 10));
    expect(s.lines).toBe(linesBefore);
    expect(s.lastSeq).toBe(seqBefore);
  });
});

describe('the exact audit scenario (Q2): drop mid-run, lines lost in the gap, reconnect', () => {
  it('ends with every produced line present exactly once', () => {
    // A busy stage produces 200 lines. The WS drops after line 80; lines
    // 81..140 are produced during the reconnect window; the stream resumes
    // at 141. Reconnect backfill must recover 81..140.
    const produced = Array.from({ length: 200 }, (_, i) => `line-${i + 1}`);
    let s = initialStageLogState();

    // Initial REST seed is empty (stage just started), first WS frames 1..80.
    s = seedFromRest(s, '');
    s = applyFrames(
      s,
      produced.slice(0, 80).map((l, i) => frame(i + 1, l)),
    );
    expect(s.lines.length).toBe(80);

    // DROP. During the gap the backend keeps capturing to disk: 1..140.
    // Reconnect → REST recovery brings 81..140 (ring evicted them).
    s = mergeReconnectBackfill(s, captureOf(produced, 140));
    expect(s.lines.length).toBe(140);

    // Stream resumes live at 141..200.
    s = applyFrames(
      s,
      produced.slice(140).map((l, i) => frame(141 + i, l)),
    );

    // No lines lost: all 200 present, in order.
    expect(s.lines).toEqual(produced);
    // No duplicates.
    expect(new Set(s.lines).size).toBe(200);
    expect(s.lastSeq).toBe(200);
  });
});
