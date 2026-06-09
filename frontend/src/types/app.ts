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
  // deployedURL is the public ingress URL written by AppHealthChecker after
  // a successful probe (W11 Indie step 6). Empty for targets that don't
  // expose an ingress (docker-host, plain kubernetes).
  deployedURL?: string;
}

export interface AppDeployResponse {
  appId: string;
  runId: string;
  channel: string;
  status: string;
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
