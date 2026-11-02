import { describe, expect, it } from 'vitest';
import { ApiError } from '../api/client';
import { logPageMatches, runtimeLogError, runtimeLogOptions } from './runtimeLogs';
import type { RuntimeLogPage } from '../types/runtimeLogs';
const input = { containerId: 'container', runId: 'accepted-run', range: '1h' as const, from: '', until: '', search: '', maxLines: 2000 };
describe('runtime log capture contract', () => {
  it('uses server time for the default and pins explicit ranges to one instant', () => {
    expect(runtimeLogOptions(input)).toEqual({ containerId: 'container', expectedRunId: 'accepted-run', maxLines: 2000 });
    expect(runtimeLogOptions({ ...input, range: '15m', search: '[REDACTED]' }, Date.parse('2026-10-03T03:00:00Z'))).toEqual({ containerId: 'container', expectedRunId: 'accepted-run', maxLines: 2000, since: '2026-10-03T02:45:00.000Z', until: '2026-10-03T03:00:00.000Z', search: '[REDACTED]' });
  });
  it('rejects missing identity, reversed/oversized ranges and oversized Unicode search', () => {
    expect(() => runtimeLogOptions({ ...input, containerId: '' })).toThrow('Choose a replica');
    expect(() => runtimeLogOptions({ ...input, range: 'custom', from: '2026-10-03T03:00:00Z', until: '2026-10-03T02:00:00Z' })).toThrow('valid time range');
    expect(() => runtimeLogOptions({ ...input, range: 'custom', from: '2026-10-01T03:00:00Z', until: '2026-10-03T03:00:00Z' })).toThrow('24 hours');
    expect(() => runtimeLogOptions({ ...input, search: 'ก'.repeat(86) })).toThrow('256 bytes');
  });
  it('rejects cross-run/container/session/cursor pages and never exposes server diagnostics', () => {
    const page = { appId: 'app', service: 'web', runId: 'run', containerId: 'container', sessionId: 'snapshot', offset: 200 } as RuntimeLogPage;
    expect(logPageMatches(page, { ...page })).toBe(true);
    for (const expected of [{ ...page, runId: 'new-run' }, { ...page, containerId: 'replacement' }, { ...page, sessionId: 'another' }, { ...page, offset: 0 }]) expect(logPageMatches(page, expected)).toBe(false);
    expect(runtimeLogError(new ApiError(404, 'private-canary'))).toContain('expired');
    expect(runtimeLogError(new ApiError(503, 'private-canary'))).not.toContain('private-canary');
  });
});
