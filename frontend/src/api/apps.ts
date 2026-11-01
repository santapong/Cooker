import { get, post, put, del, pageQuery, type PageParams } from './client';
import type {
  AppModel,
  AppDeployResponse,
  AppDeployRecord,
  AppDriftReport,
  AppCanary,
  RepositoryInspection, GitHubConnection, GitHubRepository, TargetCapability,
} from '../types/app';

export const appsApi = {
  list: (page?: PageParams) => get<AppModel[]>(`/apps${pageQuery(page)}`),
  get: (id: string) => get<AppModel>(`/apps/${id}`),
  create: (data: Partial<AppModel>) => post<AppModel>('/apps', data),
  update: (id: string, data: AppModel) => put<AppModel>(`/apps/${id}`, data),
  delete: (id: string) => del(`/apps/${id}`),
  deploy: (id: string) => post<AppDeployResponse>(`/apps/${id}/deploy`),
  inspect: (app: Partial<AppModel>) => post<RepositoryInspection>('/apps/inspect', app),
  deploymentCapabilities: () => get<{ targets: TargetCapability[]; registry?: string }>('/apps/deployment-capabilities'),
  githubConnection: () => get<GitHubConnection>('/apps/github/connection'),
  githubRepositories: (installationId: number, page = 1) => get<{ repositories: GitHubRepository[]; hasMore: boolean }>(`/apps/github/repositories?installationId=${installationId}&page=${page}`),
  githubRevisions: (repo: string, installationId = 0, kind: 'branches' | 'tags' = 'branches', page = 1) => get<{ revisions: { name: string; commit: { sha: string } }[]; hasMore: boolean }>(`/apps/github/revisions?repo=${encodeURIComponent(repo)}&installationId=${installationId}&kind=${kind}&page=${page}`),
  detectBuild: (githubRepo: string, branch: string) =>
    post<{
      plan: { kind: string; path?: string };
      suggestedRecipe: 'go' | 'node-static' | 'worker';
    }>('/apps/detect-build', { githubRepo, branch }),
  listDeploys: (id: string, limit = 20) =>
    get<{ deploys: AppDeployRecord[] }>(`/apps/${id}/deploys?limit=${limit}`),
  rollback: (id: string, deployId?: string) =>
    post<AppDeployResponse & { rolledBackTo: { deployId: string; imageRef: string } }>(
      `/apps/${id}/rollback`,
      deployId ? { deployId } : {},
    ),
  drift: (id: string) => get<AppDriftReport>(`/apps/${id}/drift`),
  setWebhookSecret: (id: string, secret: string) =>
    put<{ status: string }>(`/apps/${id}/webhook`, { secret }),
  // Canary deployments (OR-1).
  getCanary: (id: string) => get<{ canary: AppCanary }>(`/apps/${id}/canary`),
  promoteCanary: (id: string) => post<{ canary: AppCanary }>(`/apps/${id}/canary/promote`),
  abortCanary: (id: string, reason?: string) =>
    post<{ canary: AppCanary }>(`/apps/${id}/canary/abort`, reason ? { reason } : {}),
};
