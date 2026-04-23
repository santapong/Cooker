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
