import { get, post, put, del, pageQuery, type PageParams } from './client';
import type { Environment } from '../types/environment';

export interface SecretMeta {
  key: string;
  updatedAt?: string;
}

export interface SecretKeysMetadata {
  keys: string[];
  sourceAvailability: 'available' | 'unavailable';
  generation: number;
  conditionalEditing: boolean;
}

export const environmentsApi = {
  list: (page?: PageParams) => get<Environment[]>(`/environments${pageQuery(page)}`),
  create: (data: Partial<Environment>) => post<Environment>('/environments', data),
  update: (id: string, data: Partial<Environment>) =>
    put<Environment>(`/environments/${id}`, data),
  delete: (id: string) => del(`/environments/${id}`),

  // Secrets — all admin+MFA gated on the backend.
  secretKeys: (envId: string) => get<SecretKeysMetadata>(`/environments/${encodeURIComponent(envId)}/secret-keys`),
  revealSecret: (envId: string, key: string) =>
    get<{ key: string; value: string }>(
      `/environments/${encodeURIComponent(envId)}/secrets/${encodeURIComponent(key)}`,
    ),
  putSecret: (envId: string, key: string, value: string, version?: number) =>
    put<{ status: string; generation?: number }>(
      `/environments/${encodeURIComponent(envId)}/secrets/${encodeURIComponent(key)}`,
      { value, ...(version === undefined ? {} : { version }) },
    ),
  deleteSecret: (envId: string, key: string, version?: number) =>
    del(`/environments/${encodeURIComponent(envId)}/secrets/${encodeURIComponent(key)}${version === undefined ? '' : `?version=${encodeURIComponent(version)}`}`),
  promoteSecrets: (envId: string, toEnvironmentId: string) =>
    post<{ status: string; promoted: string[] }>(
      `/environments/${encodeURIComponent(envId)}/secrets/promote`,
      { toEnvironmentId },
    ),
};
