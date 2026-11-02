import type { NativeRuntimeLogPage, NativeRuntimeStatus, NativeRuntimeSubject, RuntimeReceipt } from '../types/nativeRuntime';

export function nativeSubjectKey(subject: NativeRuntimeSubject): string {
  return JSON.stringify([subject.kind, subject.pipelineId, subject.runId, subject.stageId]);
}

export function nativeSubjectMatches(actual: NativeRuntimeSubject | undefined, expected: NativeRuntimeSubject): boolean {
  return !!actual && actual.kind === 'native-stage' && actual.pipelineId === expected.pipelineId && actual.runId === expected.runId && actual.stageId === expected.stageId;
}

export function nativeStatusMatches(status: NativeRuntimeStatus, subject: NativeRuntimeSubject, receipt?: RuntimeReceipt): boolean {
  if (!nativeSubjectMatches(status.subject, subject) || status.runtime !== 'docker') return false;
  if (status.binding === 'unavailable') return true;
  if (status.binding !== 'verified' || !/^[a-f0-9]{64}$/.test(status.containerId ?? '')) return false;
  return !receipt || (status.containerId === receipt.containerId && status.name === receipt.name && status.imageId === receipt.imageId && status.imageReference === receipt.imageReference);
}

export function nativeLogPageMatches(page: NativeRuntimeLogPage, expected: { subject: NativeRuntimeSubject; containerId: string; sessionId?: string; offset?: number }): boolean {
  return !('appId' in page) && !('service' in page) && nativeSubjectMatches(page.subject, expected.subject) && page.runId === expected.subject.runId && page.containerId === expected.containerId && (!expected.sessionId || page.sessionId === expected.sessionId) && (expected.offset === undefined || page.offset === expected.offset);
}

export function hasRecordedDockerReceipt(receipt: RuntimeReceipt | undefined, runId: string, stageId: string): boolean {
  return !!receipt && receipt.version === 1 && receipt.runtime === 'docker' && receipt.runId === runId && receipt.stageId === stageId && /^[a-f0-9]{64}$/.test(receipt.containerId);
}
