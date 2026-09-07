export interface AppBuildPlan {
  kind: 'dockerfile' | 'compose' | 'buildpack';
  path?: string;
  args?: Record<string, string>;
  buildpacks?: string[];
  commit?: string;
  installationId?: number;
  files?: string[];
  profiles?: string[];
  variables?: Record<string, string>;
}

export interface AppDeployTarget {
  kind: 'docker-host' | 'kubernetes' | 'cloud-run' | 'ecs';
  hostId?: string;
  namespace?: string;
  region?: string;
  service?: string;
  prefix?: string;
  externalServices?: ExternalServiceBinding[];
}

export interface ExternalServiceBinding {
  service: string;
  provider: 'gcp-cloud-sql' | 'external';
  resource: string;
  consumers: string[];
  environment: Record<string, string>;
}

export interface RepositoryInspection {
  commit: string;
  candidates: { path: string; services: string[]; error?: string }[];
  graph?: import('./compose').ComposeGraph;
  buildFiles: { service: string; context: string; dockerfile: string; content: string }[];
  diagnostics: { service?: string; field: string; message: string }[];
  requiredVariables: string[];
  profiles: string[];
  yaml?: string;
  workloads: Record<string, string>;
  deployable: boolean;
}

export interface TargetCapability { kind: AppDeployTarget['kind']; available: boolean; reason?: string }
export interface GitHubConnection {
  configured: boolean;
  installUrl: string;
  installations: { id: number; account: { login: string } }[];
  message?: string;
}
export interface GitHubRepository { full_name: string; default_branch: string; private: boolean }

export type AppHealthStatus = 'unknown' | 'healthy' | 'degraded' | 'failed';

export type DeployStrategy = 'rolling' | 'canary';

// CanaryConfig is the opt-in canary deployment policy carried on an App
// (OR-1). The zero value (strategy "rolling") means in-place rolling
// deploys — the pre-canary behaviour.
export interface CanaryConfig {
  strategy: DeployStrategy;
  // Percent of traffic (1-99) sent to the new version while evaluating.
  weight?: number;
  // Shift to 100% automatically once the health window passes clean.
  autoPromote?: boolean;
  // Seconds the new version must stay healthy before an auto-promote.
  healthWindowSeconds?: number;
}

export type CanaryRolloutStatus = 'progressing' | 'promoted' | 'aborted' | 'failed';

// AppCanary is the live state of an in-flight canary rollout. Returned
// under "activeCanary" on GetApp and from the canary endpoints. It is
// observed progress, distinct from the CanaryConfig policy.
export interface AppCanary {
  id: string;
  appId: string;
  runId: string;
  stableImage?: string;
  canaryImage: string;
  weight: number;
  status: CanaryRolloutStatus;
  autoPromote: boolean;
  healthWindowSeconds: number;
  healthy: boolean;
  message?: string;
  promoteAfter?: string;
  startedAt: string;
  updatedAt: string;
  resolvedAt?: string;
}

export interface AppModel {
  version?: number;
  id: string;
  name: string;
  description?: string;
  githubRepo: string;
  branch: string;
  buildPlan?: AppBuildPlan | null;
  deployTarget: AppDeployTarget;
  registryRef?: string;
  environmentId?: string;
  hasWebhook: boolean;
  autoDeploy: boolean;
  // Canary deployment policy (OR-1). Always present in responses
  // (normalised server-side); defaults to { strategy: 'rolling' }.
  canary: CanaryConfig;
  // activeCanary is the live rollout state, present only while a canary
  // is in flight (embedded on GetApp so the detail page renders the
  // panel on first load).
  activeCanary?: AppCanary | null;
  createdAt: string;
  updatedAt: string;
  // Post-deploy readiness verdict from the backend AppHealthChecker.
  // "unknown" until the first probe runs or when the target kind has
  // no probe wired in the registry.
  healthStatus?: AppHealthStatus;
  healthCheckedAt?: string;
  healthMessage?: string;
  // deployedURL is the public ingress URL written by AppHealthChecker after
  // a successful probe (W11 Indie step 6). Empty for targets that don't
  // expose an ingress (docker-host, plain kubernetes).
  deployedURL?: string;
}

export interface AppDeployResponse {
  pipelineId?: string;
  deploymentView?: string;
  appId: string;
  runId: string;
  channel: string;
  status: string;
  // strategy is "rolling" or "canary" — which deploy path the click took.
  strategy?: DeployStrategy;
  stream: string;
  repo: string;
  branch: string;
  // url is the public ingress URL for the deployed service (Indie step 6,
  // W11-A2). Optional: docker-host targets without an ingress do not set
  // this field. The backend surfaces it from DeployTarget.Status.URL.
  url?: string;
}

export interface AppDeployRecord {
  id: string;
  appId: string;
  runId: string;
  pipelineId?: string;
  imageRef?: string;
  digest?: string;
  status: string;
  kind: 'deploy' | 'rollback';
  createdAt: string;
}

export interface AppDriftReport {
  status: 'in_sync' | 'drift' | 'unknown' | 'unsupported';
  expectedImage?: string;
  liveImage?: string;
  message?: string;
  checkedAt: string;
}
