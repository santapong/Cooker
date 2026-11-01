import { describe, expect, it } from 'vitest';
import type { PipelineEdge, PipelineRun, Stage, StageRun } from '../../types/pipeline';
import {
  edgeStateFor,
  elapsedMs,
  formatDuration,
  isTerminal,
  runProgress,
  runSub,
  stageRunMap,
  starStatusFor,
  starSub,
  telemetryLines,
} from './runState';

const sr = (stageId: string, status: StageRun['status'], extra: Partial<StageRun> = {}): StageRun => ({ stageId, status, ...extra });
const stage = (id: string, type: Stage['type'] = 'custom'): Stage => ({ id, name: id, type, config: {}, position: { x: 0, y: 0 } });
const edge = (source: string, target: string, condition?: PipelineEdge['condition']): PipelineEdge => ({ id: `${source}-${target}`, source, target, condition });

describe('terminal + status lookup', () => {
  it('knows which statuses end a run', () => {
    expect(isTerminal('success')).toBe(true);
    expect(isTerminal('failed')).toBe(true);
    expect(isTerminal('cancelled')).toBe(true);
    expect(isTerminal('running')).toBe(false);
    expect(isTerminal('pending')).toBe(false);
    expect(isTerminal(undefined)).toBe(false);
  });

  it('stars are idle when the run has no record for them', () => {
    const byId = stageRunMap({ stageRuns: [sr('a', 'running')] } as unknown as PipelineRun);
    expect(starStatusFor('a', byId)).toBe('running');
    expect(starStatusFor('zzz', byId)).toBe('idle');
  });
});

describe('edgeStateFor', () => {
  it('lights up once the source succeeded and is hot while the target runs', () => {
    const byId = stageRunMap({ stageRuns: [sr('a', 'success'), sr('b', 'running'), sr('c', 'pending')] } as unknown as PipelineRun);
    expect(edgeStateFor(edge('a', 'b'), byId)).toBe('hot');
    expect(edgeStateFor(edge('a', 'c'), byId)).toBe('done');
    expect(edgeStateFor(edge('b', 'c'), byId)).toBe('idle');
  });

  it('respects the edge condition', () => {
    const byId = stageRunMap({ stageRuns: [sr('a', 'failed'), sr('b', 'running'), sr('c', 'cancelled'), sr('d', 'running')] } as unknown as PipelineRun);
    expect(edgeStateFor(edge('a', 'b'), byId)).toBe('idle');
    expect(edgeStateFor(edge('a', 'b', 'failure'), byId)).toBe('hot');
    expect(edgeStateFor(edge('c', 'd', 'always'), byId)).toBe('hot');
    expect(edgeStateFor(edge('c', 'd'), byId)).toBe('idle');
  });
});

describe('progress + timing', () => {
  it('counts settled stages and lists the running ones', () => {
    const stages = [stage('a'), stage('b'), stage('c'), stage('d')];
    const byId = stageRunMap({ stageRuns: [sr('a', 'success'), sr('b', 'running'), sr('c', 'skipped')] } as unknown as PipelineRun);
    expect(runProgress(stages, byId)).toEqual({ done: 2, total: 4, running: ['b'] });
  });

  it('formats durations', () => {
    expect(formatDuration(0)).toBe('00:00');
    expect(formatDuration(64_000)).toBe('01:04');
    expect(formatDuration(3_725_000)).toBe('1:02:05');
    expect(formatDuration(null)).toBe('--:--');
    expect(formatDuration(-5)).toBe('--:--');
  });

  it('elapsed falls back to the earliest stage start and stops at finish', () => {
    const run = {
      startedAt: null,
      finishedAt: '2026-09-05T10:00:30Z',
      stageRuns: [sr('a', 'success', { startedAt: '2026-09-05T10:00:05Z' }), sr('b', 'success', { startedAt: '2026-09-05T10:00:00Z' })],
    } as unknown as PipelineRun;
    expect(elapsedMs(run, Date.parse('2026-09-05T11:00:00Z'))).toBe(30_000);
    expect(elapsedMs({ stageRuns: [] } as unknown as PipelineRun, 0)).toBeNull();
  });

  it('star sub-label: queued / status / duration / config fact', () => {
    const now = Date.parse('2026-09-05T10:00:10Z');
    const s = stage('build', 'build');
    expect(starSub(s, undefined, now)).toBe('image');
    expect(starSub(s, sr('build', 'pending'), now)).toBe('queued');
    expect(starSub(s, sr('build', 'skipped'), now)).toBe('skipped');
    expect(starSub(s, sr('build', 'running', { startedAt: '2026-09-05T10:00:00Z' }), now)).toBe('00:10');
    expect(starSub(s, sr('build', 'success', { startedAt: '2026-09-05T10:00:00Z', finishedAt: '2026-09-05T10:01:04Z' }), now)).toBe('01:04');
    expect(starSub(s, sr('build', 'running'), now)).toBe('running');
    expect(runSub('build', {}, 'idle', null, null, now)).toBe('image');
    expect(runSub('build', {}, undefined, null, null, now)).toBe('image');
    expect(runSub('test', { image: 'k6' }, 'running', '2026-09-05T10:00:00Z', null, now)).toBe('00:10');
  });
});

describe('telemetryLines', () => {
  it('orders transitions by time and tones them by outcome', () => {
    const run = {
      status: 'failed',
      finishedAt: '2026-09-05T10:00:20Z',
      error: 'stage checkout failed',
      stageRuns: [
        sr('b', 'failed', { startedAt: '2026-09-05T10:00:10Z', finishedAt: '2026-09-05T10:00:20Z', error: 'boom' }),
        sr('a', 'success', { startedAt: '2026-09-05T10:00:00Z', finishedAt: '2026-09-05T10:00:05Z' }),
        sr('c', 'running', { startedAt: '2026-09-05T10:00:06Z' }),
      ],
    } as unknown as PipelineRun;
    const lines = telemetryLines(run, [stage('a'), stage('b'), stage('c')]);
    expect(lines.map((l) => l.text)).toEqual([
      'a — started',
      'a — success in 00:05',
      'c — started',
      'b — started',
      'b — failed in 00:10: boom',
      'run — failed: stage checkout failed',
    ]);
    expect(lines.map((l) => l.tone)).toEqual(['info', 'ok', 'hot', 'info', 'fail', 'fail']);
    expect(telemetryLines(null, [])).toEqual([]);
  });
});
