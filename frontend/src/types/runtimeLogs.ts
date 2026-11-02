export interface RuntimeLogReplica { containerId: string; name: string; state: string }
export interface RuntimeLogReplicas { appId: string; service: string; runId: string; replicas: RuntimeLogReplica[] }
export interface RuntimeLogOptions { containerId: string; expectedRunId: string; since?: string; until?: string; search?: string; maxLines: number }
export interface RuntimeLogEntry { sequence: number; timestamp?: string; message: string }
export interface RuntimeLogPage {
  sessionId: string;
  appId: string;
  service: string;
  runId: string;
  containerId: string;
  since: string;
  until: string;
  expiresAt: string;
  offset: number;
  nextOffset?: number;
  total: number;
  tailLimitReached: boolean;
  coverage: string;
  entries: RuntimeLogEntry[];
}
