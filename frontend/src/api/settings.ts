import { get, post, del } from './client';
import type { RegistryConfig, ClusterConfig } from '../types/infra';

export const settingsApi = {
  listRegistries: () => get<RegistryConfig[]>('/settings/registries'),
  addRegistry: (data: { name: string; url: string; username?: string; password?: string }) =>
    post<{ message: string; name: string }>('/settings/registries', data),
  deleteRegistry: (id: string) => del(`/settings/registries/${id}`),

  listClusters: () => get<ClusterConfig[]>('/settings/clusters'),
  addCluster: (data: { name: string; kubeconfig?: string; context?: string }) =>
    post<{ message: string; name: string }>('/settings/clusters', data),
};
