export type StageType = 'build' | 'test' | 'deploy' | 'push' | 'approval' | 'custom';
export type RunStatus = 'pending' | 'running' | 'success' | 'failed' | 'cancelled';

export interface Pipeline {
  id: string;
  name: string;
  description: string;
  stages: Stage[];
  edges: PipelineEdge[];
  variables: Record<string, string>;
  createdAt: string;
  updatedAt: string;
}

export interface Stage {
  id: string;
  name: string;
  type: StageType;
  config: StageConfig;
  environmentId?: string;
  position: { x: number; y: number };
}

export interface StageConfig {
  // Build
  dockerfile?: string;
  context?: string;
  buildArgs?: Record<string, string>;
  tags?: string[];
  platforms?: string[];
  // Test
  image?: string;
  command?: string[];
  // Push
  registry?: string;
  repository?: string;
  // Deploy
  namespace?: string;
  manifestPath?: string;
  helmChart?: string;
  helmValues?: Record<string, unknown>;
  // Custom
  script?: string;
  timeout?: string;
  retries?: number;
}

export interface PipelineEdge {
  id: string;
  source: string;
  target: string;
  condition?: 'success' | 'failure' | 'always';
}

export interface PipelineRun {
  id: string;
  pipelineId: string;
  status: RunStatus;
  stageRuns: StageRun[];
  environmentStatuses: EnvironmentStatus[];
  variables: Record<string, string>;
  startedAt?: string;
  finishedAt?: string;
  error?: string;
}

export interface StageRun {
  stageId: string;
  status: RunStatus;
  startedAt?: string;
  finishedAt?: string;
  logs?: string;
  error?: string;
  artifacts?: Artifact[];
}

export interface Artifact {
  type: 'oci-image' | 'test-report' | 'log';
  ref: string;
  digest: string;
}

export interface EnvironmentStatus {
  environmentId: string;
  status: 'pending' | 'deploying' | 'deployed' | 'failed' | 'awaiting_approval';
  promotedAt?: string;
  approvedBy?: string;
}
