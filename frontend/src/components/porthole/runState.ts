import type { PipelineEdge, PipelineRun, RunStatus, Stage, StageConfig, StageRun, StageType } from '../../types/pipeline';
import type { StarStatus } from './StarNode';
import type { BadgeVariant } from '../ui/Badge';
import { stageSub } from './constellation';

/** Light on a constellation line: idle (dark), done (passed), hot (passing now). */
export type EdgeState = 'idle' | 'done' | 'hot';

const TERMINAL: ReadonlySet<string> = new Set<RunStatus>(['success', 'failed', 'cancelled']);

export function isTerminal(status: RunStatus | string | undefined | null): boolean {
  return !!status && TERMINAL.has(status);
}

export function stageRunMap(run: PipelineRun | null | undefined): Map<string, StageRun> {
  const m = new Map<string, StageRun>();
  for (const sr of run?.stageRuns ?? []) m.set(sr.stageId, sr);
  return m;
}

export function starStatusFor(stageId: string, byId: Map<string, StageRun>): StarStatus {
  return byId.get(stageId)?.status ?? 'idle';
}

/**
 * An edge carries light once its source has resolved the way the edge
 * condition wants: success (default), failure, or any terminal state for
 * `always`. It is *hot* while the target is running.
 */
export function edgeStateFor(edge: PipelineEdge, byId: Map<string, StageRun>): EdgeState {
  const s = byId.get(edge.source)?.status;
  const t = byId.get(edge.target)?.status;
  const passed =
    edge.condition === 'always'
      ? isTerminal(s)
      : edge.condition === 'failure'
        ? s === 'failed'
        : s === 'success';
  if (!passed) return 'idle';
  return t === 'running' ? 'hot' : 'done';
}

export function runProgress(stages: Stage[], byId: Map<string, StageRun>): { done: number; total: number; running: string[] } {
  let done = 0;
  const running: string[] = [];
  for (const s of stages) {
    const st = byId.get(s.id)?.status;
    if (isTerminal(st) || st === 'skipped') done++;
    if (st === 'running') running.push(s.id);
  }
  return { done, total: stages.length, running };
}

function ts(v: string | null | undefined): number | null {
  if (!v) return null;
  const n = Date.parse(v);
  return Number.isFinite(n) ? n : null;
}

/** Run wall-clock: from the run start (or the earliest stage start) to finish or `now`. */
export function elapsedMs(run: PipelineRun, now: number): number | null {
  let start = ts(run.startedAt);
  if (start === null) {
    for (const sr of run.stageRuns) {
      const s = ts(sr.startedAt);
      if (s !== null && (start === null || s < start)) start = s;
    }
  }
  if (start === null) return null;
  const end = ts(run.finishedAt) ?? now;
  return Math.max(0, end - start);
}

/** mm:ss under an hour, h:mm:ss above; `--:--` when unknown. */
export function formatDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || !Number.isFinite(ms) || ms < 0) return '--:--';
  const total = Math.floor(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const mm = String(m).padStart(2, '0');
  const ss = String(s).padStart(2, '0');
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}

export function stageDurationMs(sr: StageRun | undefined, now: number): number | null {
  const start = ts(sr?.startedAt);
  if (start === null) return null;
  return Math.max(0, (ts(sr?.finishedAt) ?? now) - start);
}

/** Mono sub-label under a star in the run view: a duration, a state word, or the config fact. */
export function starSub(stage: Stage, sr: StageRun | undefined, now: number): string {
  return runSub(stage.type, stage.config, sr?.status, sr?.startedAt, sr?.finishedAt, now);
}

/** Same as starSub, on the raw fields a StarNode carries in its data. */
export function runSub(
  type: StageType,
  config: StageConfig,
  status: RunStatus | 'idle' | undefined,
  startedAt: string | null | undefined,
  finishedAt: string | null | undefined,
  now: number,
): string {
  if (!status || status === 'idle') return stageSub(type, config);
  switch (status) {
    case 'pending':
      return 'queued';
    case 'skipped':
    case 'cancelled':
      return status;
    default: {
      const start = ts(startedAt);
      if (start === null) return status;
      return formatDuration(Math.max(0, (ts(finishedAt) ?? now) - start));
    }
  }
}

export interface TelemetryLine {
  at: number;
  time: string;
  text: string;
  tone: 'info' | 'ok' | 'fail' | 'hot';
}

function clock(at: number): string {
  const d = new Date(at);
  return [d.getHours(), d.getMinutes(), d.getSeconds()].map((n) => String(n).padStart(2, '0')).join(':');
}

/** Run-level telemetry: stage start/finish transitions in time order, with errors. */
export function telemetryLines(run: PipelineRun | null | undefined, stages: Stage[]): TelemetryLine[] {
  if (!run) return [];
  const nameOf = new Map(stages.map((s) => [s.id, s.name]));
  const out: TelemetryLine[] = [];
  for (const sr of run.stageRuns) {
    const name = nameOf.get(sr.stageId) ?? sr.stageId;
    const start = ts(sr.startedAt);
    const end = ts(sr.finishedAt);
    if (start !== null) {
      out.push({ at: start, time: clock(start), text: `${name} — started`, tone: end === null && sr.status === 'running' ? 'hot' : 'info' });
    }
    if (end !== null) {
      const dur = start !== null ? ` in ${formatDuration(end - start)}` : '';
      const err = sr.error ? `: ${sr.error}` : '';
      out.push({
        at: end,
        time: clock(end),
        text: `${name} — ${sr.status}${dur}${err}`,
        tone: sr.status === 'success' ? 'ok' : sr.status === 'failed' ? 'fail' : 'info',
      });
    }
  }
  const finished = ts(run.finishedAt);
  if (finished !== null) {
    out.push({
      at: finished,
      time: clock(finished),
      text: `run — ${run.status}${run.error ? `: ${run.error}` : ''}`,
      tone: run.status === 'success' ? 'ok' : run.status === 'failed' ? 'fail' : 'info',
    });
  }
  return out.sort((a, b) => a.at - b.at);
}

/** Badge variant for a run / stage status. */
export function statusVariant(status: string | undefined | null): BadgeVariant {
  switch (status) {
    case 'success':
      return 'ok';
    case 'failed':
      return 'fail';
    case 'running':
      return 'running';
    default:
      return 'muted';
  }
}
