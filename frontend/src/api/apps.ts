import { get, post, put, del } from './client';
import type {
  AppModel,
  AppDeployResponse,
  AppDeployRecord,
  AppDriftReport,
  AppCanary,
} from '../types/app';

export const appsApi = {
  list: () => get<AppModel[]>('/apps'),
  get: (id: string) => get<AppModel>(`/apps/${id}`),
  create: (data: Partial<AppModel>) => post<AppModel>('/apps', data),
  update: (id: string, data: AppModel) => put<AppModel>(`/apps/${id}`, data),
  delete: (id: string) => del(`/apps/${id}`),
  deploy: (id: string) => post<AppDeployResponse>(`/apps/${id}/deploy`),
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
