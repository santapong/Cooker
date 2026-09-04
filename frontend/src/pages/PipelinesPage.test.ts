import { describe, expect, it } from 'vitest';
import type { PipelineRun } from '../types/pipeline';
import { runChartStatus } from './PipelinesPage';

const run = (status: PipelineRun['status'], stageStatuses: PipelineRun['status'][] = []): PipelineRun =>
  ({ id: 'r', pipelineId: 'p', status, stageRuns: stageStatuses.map((s, i) => ({ stageId: `s${i}`, status: s })), environmentStatuses: [], variables: {} }) as PipelineRun;

describe('runChartStatus', () => {
  it('maps the latest run onto the row status star', () => {
    expect(runChartStatus(undefined)).toBe('idle');
    expect(runChartStatus(run('success'))).toBe('ok');
    expect(runChartStatus(run('failed'))).toBe('fail');
    expect(runChartStatus(run('running'))).toBe('running');
    // a pending run with a live stage (an approval gate) is running
    expect(runChartStatus(run('pending', ['success', 'running', 'pending']))).toBe('running');
    expect(runChartStatus(run('pending', ['pending']))).toBe('idle');
    expect(runChartStatus(run('cancelled'))).toBe('idle');
  });
});
