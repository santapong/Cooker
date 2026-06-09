import { get, post, put, del } from './client';
import type { AppModel, AppDeployResponse } from '../types/app';

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
  setWebhookSecret: (id: string, secret: string) =>
    put<{ status: string }>(`/apps/${id}/webhook`, { secret }),
};
