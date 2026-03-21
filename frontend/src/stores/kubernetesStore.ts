import { create } from 'zustand';
import type { KubeNamespace, KubeWorkload } from '../types/kubernetes';
import { kubernetesApi } from '../api/kubernetes';

interface KubernetesStore {
  namespaces: KubeNamespace[];
  workloads: KubeWorkload[];
  selectedNamespace: string;
  loading: boolean;
  error: string | null;

  fetchNamespaces: () => Promise<void>;
  fetchWorkloads: (namespace?: string) => Promise<void>;
  setNamespace: (ns: string) => void;
  scale: (ns: string, kind: string, name: string, replicas: number) => Promise<void>;
  restart: (ns: string, kind: string, name: string) => Promise<void>;
}

export const useKubernetesStore = create<KubernetesStore>((set, get) => ({
  namespaces: [],
  workloads: [],
  selectedNamespace: 'default',
  loading: false,
  error: null,

  fetchNamespaces: async () => {
    set({ loading: true });
    try {
      const namespaces = await kubernetesApi.listNamespaces();
      set({ namespaces, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  fetchWorkloads: async (namespace?: string) => {
    set({ loading: true });
    try {
      const ns = namespace || get().selectedNamespace;
      const workloads = await kubernetesApi.listWorkloads(ns);
      set({ workloads, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  setNamespace: (ns: string) => {
    set({ selectedNamespace: ns });
    get().fetchWorkloads(ns);
  },

  scale: async (ns, kind, name, replicas) => {
    await kubernetesApi.scale(ns, kind, name, replicas);
    get().fetchWorkloads();
  },

  restart: async (ns, kind, name) => {
    await kubernetesApi.restart(ns, kind, name);
  },
}));
