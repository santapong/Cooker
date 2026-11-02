import { del, get, getText, post } from './client';
import type { RuntimeLogOptions, RuntimeLogPage, RuntimeLogReplicas } from '../types/runtimeLogs';

function servicePath(appId: string, service: string): string {
  return `/apps/${encodeURIComponent(appId)}/services/${encodeURIComponent(service)}`;
}
function sessionPath(appId: string, service: string, sessionId: string): string {
  return `${servicePath(appId, service)}/log-sessions/${encodeURIComponent(sessionId)}`;
}
export const runtimeLogsApi = {
  replicas: (appId: string, service: string) => get<RuntimeLogReplicas>(`${servicePath(appId, service)}/log-replicas`),
  open: (appId: string, service: string, options: RuntimeLogOptions) => post<RuntimeLogPage>(`${servicePath(appId, service)}/log-sessions`, options),
  page: (appId: string, service: string, sessionId: string, offset: number) => get<RuntimeLogPage>(`${sessionPath(appId, service, sessionId)}?offset=${offset}&limit=200`),
  download: (appId: string, service: string, sessionId: string) => getText(`${sessionPath(appId, service, sessionId)}/download`, 'text/plain'),
  close: (appId: string, service: string, sessionId: string) => del<void>(sessionPath(appId, service, sessionId)),
};
