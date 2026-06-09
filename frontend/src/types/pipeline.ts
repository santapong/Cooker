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
  // group is the deployment group-box this stage belongs to (compose
  // deployment DAGs). Empty/undefined means ungrouped.
  group?: string;
}

export interface ResourceLimits {
  memory?: string;
  memoryBytes?: number;
  cpus?: string;
  nanoCpus?: number;
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
  // Compose deployment DAG provenance + per-service deploy runtime.
  deployRuntime?: 'kubernetes' | 'docker' | 'compose';
  composeServiceName?: string;
  composeBuildContext?: string;
  composeDockerfile?: string;
  resources?: ResourceLimits;
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
  /** inter-stage outputs; consumed downstream via ${stages.<id>.<key>} */
  outputs?: Record<string, string>;
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

// Stage-duration analytics (roadmap M4): GET /pipelines/:id/analytics
export interface PipelineAnalytics {
  pipelineId: string;
  runCount: number;
  successRate: number;
  stages: Array<{
    stageId: string;
    name?: string;
    type?: string;
    samples: number;
    p50Ms: number;
    p95Ms: number;
    avgMs: number;
    successRate: number;
  }>;
  runs: Array<{
    runId: string;
    createdAt: string;
    status: RunStatus;
    durationMs: number;
    stageDurations?: Record<string, number>;
  }>;
}
