import { ApiError } from '../api/client';
import type { RuntimeLogOptions, RuntimeLogPage } from '../types/runtimeLogs';

export type LogRange = '1h' | '15m' | '6h' | '24h' | 'custom';
export function runtimeLogOptions(input: { containerId: string; runId: string; range: LogRange; from: string; until: string; search: string; maxLines: number }, now = Date.now()): RuntimeLogOptions {
  if (!input.containerId || !input.runId) throw new Error('Choose a replica before capturing logs.');
  if (new TextEncoder().encode(input.search).length > 256) throw new Error('Search must fit within 256 bytes.');
  const options: RuntimeLogOptions = { containerId: input.containerId, expectedRunId: input.runId, maxLines: input.maxLines };
  if (input.search) options.search = input.search;
  // The default uses server time. Explicit ranges retain one common end time.
  if (input.range !== '1h') {
    const end = input.range === 'custom' ? Date.parse(input.until) : now;
    const start = input.range === 'custom' ? Date.parse(input.from) : end - ({ '15m': 900000, '6h': 21600000, '24h': 86400000 }[input.range]);
    if (!Number.isFinite(start) || !Number.isFinite(end) || start >= end || end - start > 86400000) throw new Error('Choose a valid time range of up to 24 hours.');
    options.since = new Date(start).toISOString();
    options.until = new Date(end).toISOString();
  }
  return options;
}

export function logPageMatches(page: RuntimeLogPage, expected: { appId: string; service: string; runId: string; containerId: string; sessionId?: string; offset?: number }): boolean {
  return page.appId === expected.appId && page.service === expected.service && page.runId === expected.runId && page.containerId === expected.containerId && (!expected.sessionId || page.sessionId === expected.sessionId) && (expected.offset === undefined || page.offset === expected.offset);
}

export function runtimeLogError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 404) return 'This snapshot expired or is no longer available. Capture a new snapshot.';
    if (error.status === 403) return 'Your role cannot read these runtime logs.';
    if (error.status === 429) return 'Log capacity is busy. Close a snapshot or retry after expiry.';
    if (error.status === 400) return 'Check the selected replica, search and time range before retrying.';
    if (error.status === 503) return 'Logs are unavailable for this deployment. Refresh replicas before retrying.';
  }
  return 'Logs could not be read. Retry the same snapshot or refresh replicas.';
}
