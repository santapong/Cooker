export interface AppBuildPlan {
  kind: 'dockerfile' | 'compose' | 'buildpack';
  path?: string;
  args?: Record<string, string>;
  buildpacks?: string[];
}

export interface AppDeployTarget {
  kind: 'docker-host' | 'kubernetes' | 'cloud-run';
  hostId?: string;
  namespace?: string;
  region?: string;
  service?: string;
}

export type AppHealthStatus = 'unknown' | 'healthy' | 'degraded' | 'failed';

export interface AppModel {
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
  createdAt: string;
  updatedAt: string;
  // Post-deploy readiness verdict from the backend AppHealthChecker.
  // "unknown" until the first probe runs or when the target kind has
  // no probe wired in the registry.
  healthStatus?: AppHealthStatus;
  healthCheckedAt?: string;
  healthMessage?: string;
}

export interface AppDeployResponse {
  appId: string;
  runId: string;
  channel: string;
  status: string;
  stream: string;
  repo: string;
  branch: string;
}
