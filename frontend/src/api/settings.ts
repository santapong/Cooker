import { get, post, patch, del } from './client';
import type { RegistryConfig, RegistryGrantUpdate, ClusterConfig } from '../types/infra';

/** Outcome of one secrets-backend connectivity probe (admin+MFA). */
export interface SecretsCheckResult {
  backend: string;
  ok: boolean;
  latencyMs: number;
  error?: string;
}

export const settingsApi = {
  listRegistries: () => get<RegistryConfig[]>('/settings/registries'),
  addRegistry: (data: { name: string; url: string; username?: string; password?: string }) =>
    post<RegistryConfig>('/settings/registries', data),
  updateRegistryGrants: (id: string, data: RegistryGrantUpdate) =>
    patch<RegistryConfig>(`/settings/registries/${encodeURIComponent(id)}/grants`, data),
  deleteRegistry: (id: string) => del(`/settings/registries/${id}`),

  listClusters: () => get<ClusterConfig[]>('/settings/clusters'),
  addCluster: (data: { name: string; kubeconfig?: string; context?: string }) =>
    post<{ message: string; name: string }>('/settings/clusters', data),

  testSecrets: (environmentId?: string) =>
    post<SecretsCheckResult>('/settings/secrets/test', environmentId ? { environmentId } : {}),
};
