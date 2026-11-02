import type { RunStatus } from './pipeline';
import type { RuntimeLogPage, RuntimeLogReplica } from './runtimeLogs';

export interface RuntimeReceipt {
  version: number;
  runtime: string;
  runId: string;
  stageId: string;
  containerId: string;
  name: string;
  imageReference: string;
  imageId: string;
  capturedAt: string;
}

export interface NativeRuntimeSubject {
  kind: 'native-stage';
  pipelineId: string;
  runId: string;
  stageId: string;
}

export interface NativeRuntimeStatus {
  subject: NativeRuntimeSubject;
  recordedStageStatus: RunStatus | '';
  binding: 'verified' | 'unavailable';
  reasonCode?: string;
  runtime: 'docker';
  containerId?: string;
  name?: string;
  imageReference?: string;
  imageId?: string;
  state: string;
  health: 'healthy' | 'unhealthy' | 'starting' | 'not_configured' | 'unknown';
  observedAt?: string;
  supportedActions: string[];
}

export interface NativeRuntimeLogReplicas {
  subject: NativeRuntimeSubject;
  runId: string;
  replicas: RuntimeLogReplica[];
}

export interface NativeRuntimeLogPage extends Omit<RuntimeLogPage, 'appId' | 'service'> {
  subject: NativeRuntimeSubject;
}
