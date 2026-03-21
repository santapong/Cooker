import { create } from 'zustand';
import type { Environment } from '../types/environment';
import { get as apiGet, post, put, del } from '../api/client';

interface EnvironmentStore {
  environments: Environment[];
  loading: boolean;
  error: string | null;

  fetchEnvironments: () => Promise<void>;
  createEnvironment: (env: Partial<Environment>) => Promise<void>;
  updateEnvironment: (id: string, env: Environment) => Promise<void>;
  deleteEnvironment: (id: string) => Promise<void>;
  promote: (pipelineId: string, runId: string) => Promise<void>;
  approve: (pipelineId: string, runId: string, approvedBy: string) => Promise<void>;
}

export const useEnvironmentStore = create<EnvironmentStore>((set, get) => ({
  environments: [],
  loading: false,
  error: null,

  fetchEnvironments: async () => {
    set({ loading: true });
    try {
      const environments = await apiGet<Environment[]>('/environments');
      set({ environments, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  createEnvironment: async (env) => {
    await post('/environments', env);
    get().fetchEnvironments();
  },

  updateEnvironment: async (id, env) => {
    await put(`/environments/${id}`, env);
    get().fetchEnvironments();
  },

  deleteEnvironment: async (id) => {
    await del(`/environments/${id}`);
    set((state) => ({
      environments: state.environments.filter((e) => e.id !== id),
    }));
  },

  promote: async (pipelineId, runId) => {
    await post(`/pipelines/${pipelineId}/runs/${runId}/promote`);
  },

  approve: async (pipelineId, runId, approvedBy) => {
    await post(`/pipelines/${pipelineId}/runs/${runId}/approve`, { approvedBy });
  },
}));
