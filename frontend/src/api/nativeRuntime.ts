import { del, get, getText, post } from './client';
import type { NativeRuntimeLogPage, NativeRuntimeLogReplicas, NativeRuntimeStatus, NativeRuntimeSubject } from '../types/nativeRuntime';
import type { RuntimeLogOptions } from '../types/runtimeLogs';

export function nativeStagePath(subject: NativeRuntimeSubject): string {
  return `/pipelines/${encodeURIComponent(subject.pipelineId)}/runs/${encodeURIComponent(subject.runId)}/stages/${encodeURIComponent(subject.stageId)}`;
}

export function nativeRuntimeLogsPath(subject: NativeRuntimeSubject): string {
  return `/ws${nativeStagePath(subject)}/runtime-logs`;
}

function sessionPath(subject: NativeRuntimeSubject, sessionId: string): string {
  return `${nativeStagePath(subject)}/log-sessions/${encodeURIComponent(sessionId)}`;
}

export const nativeRuntimeApi = {
  status: (subject: NativeRuntimeSubject) => get<NativeRuntimeStatus>(`${nativeStagePath(subject)}/runtime`),
  replicas: (subject: NativeRuntimeSubject) => get<NativeRuntimeLogReplicas>(`${nativeStagePath(subject)}/log-replicas`),
  open: (subject: NativeRuntimeSubject, options: RuntimeLogOptions) => post<NativeRuntimeLogPage>(`${nativeStagePath(subject)}/log-sessions`, options),
  page: (subject: NativeRuntimeSubject, sessionId: string, offset: number) => get<NativeRuntimeLogPage>(`${sessionPath(subject, sessionId)}?offset=${offset}&limit=200`),
  download: (subject: NativeRuntimeSubject, sessionId: string) => getText(`${sessionPath(subject, sessionId)}/download`, 'text/plain'),
  close: (subject: NativeRuntimeSubject, sessionId: string) => del<void>(sessionPath(subject, sessionId)),
};
