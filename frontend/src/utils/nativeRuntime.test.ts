import { expect, it } from 'vitest';
import { hasRecordedDockerReceipt, nativeLogPageMatches, nativeStatusMatches, nativeSubjectKey } from './nativeRuntime';
import type { NativeRuntimeLogPage, NativeRuntimeStatus, NativeRuntimeSubject, RuntimeReceipt } from '../types/nativeRuntime';

const subject: NativeRuntimeSubject = { kind: 'native-stage', pipelineId: 'p:one', runId: 'r', stageId: 'web' };
const receipt: RuntimeReceipt = { version: 1, runtime: 'docker', runId: 'r', stageId: 'web', containerId: 'a'.repeat(64), name: 'recorded-web', imageReference: 'fixture/image:one', imageId: `sha256:${'b'.repeat(64)}`, capturedAt: '2026-10-05T00:00:00Z' };
const status: NativeRuntimeStatus = { subject, recordedStageStatus: 'success', binding: 'verified', runtime: 'docker', containerId: receipt.containerId, name: receipt.name, imageId: receipt.imageId, imageReference: receipt.imageReference, state: 'running', health: 'unknown', supportedActions: ['inspect'] };

it('uses the actual verified binding and preserves health uncertainty', () => {
  expect(nativeStatusMatches(status, subject, receipt)).toBe(true);
  expect(status.health).toBe('unknown');
  expect(nativeStatusMatches({ ...status, binding: 'available' } as unknown as NativeRuntimeStatus, subject, receipt)).toBe(false);
  expect(nativeStatusMatches({ ...status, containerId: 'c'.repeat(64) }, subject, receipt)).toBe(false);
  expect(nativeStatusMatches({ ...status, subject: { ...subject, runId: 'other' } }, subject, receipt)).toBe(false);
  expect(nativeStatusMatches({ ...status, binding: 'unavailable', containerId: undefined }, subject, receipt)).toBe(true);
});

it('does not confuse colon tuples, literal percent IDs, or provider/App subjects', () => {
  expect(nativeSubjectKey(subject)).not.toBe(nativeSubjectKey({ ...subject, pipelineId: 'p', runId: 'one:r' }));
  expect(nativeSubjectKey({ ...subject, pipelineId: 'p%41' })).not.toBe(nativeSubjectKey({ ...subject, pipelineId: 'pA' }));
  expect(hasRecordedDockerReceipt(receipt, 'r', 'web')).toBe(true);
  expect(hasRecordedDockerReceipt(receipt, 'other', 'web')).toBe(false);
  expect(hasRecordedDockerReceipt({ ...receipt, version: 2 }, 'r', 'web')).toBe(false);
});

it('rejects a native snapshot that switches subject, container, cursor or cache kind', () => {
  const page: NativeRuntimeLogPage = { subject, sessionId: 'snapshot', runId: 'r', containerId: receipt.containerId, since: '', until: '', expiresAt: '', offset: 0, total: 0, coverage: '', tailLimitReached: false, entries: [] };
  const expected = { subject, containerId: receipt.containerId, sessionId: 'snapshot', offset: 0 };
  expect(nativeLogPageMatches(page, expected)).toBe(true);
  expect(nativeLogPageMatches({ ...page, subject: { ...subject, stageId: 'other' } }, expected)).toBe(false);
  expect(nativeLogPageMatches({ ...page, containerId: 'c'.repeat(64) }, expected)).toBe(false);
  expect(nativeLogPageMatches({ ...page, offset: 200 }, expected)).toBe(false);
  expect(nativeLogPageMatches({ ...page, appId: 'synthetic-app' } as NativeRuntimeLogPage, expected)).toBe(false);
});
