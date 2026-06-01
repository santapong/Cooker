import { create } from 'zustand';
import type { KubeNamespace, KubeWorkload } from '../types/kubernetes';
import { kubernetesApi } from '../api/kubernetes';
import { ApiError } from '../api/client';

// clusterUnavailable means "no cluster configured or unreachable" and
// maps to HTTP 503 ONLY. The k8s read routes are gated at operator role,
// so a 403 (not permitted), a 500 (server fault), or a network timeout
// must surface as a plain error — not the misleading "cluster
// unreachable" banner. This helper sets the right state for either case.
function applyFetchError(e: unknown): {
  error: string;
  loading: false;
  clusterUnavailable: boolean;
} {
  const is503 = e instanceof ApiError && e.status === 503;
  return {
    error: e instanceof Error ? e.message : String(e),
    loading: false,
    clusterUnavailable: is503,
  };
}

interface KubernetesStore {
  namespaces: KubeNamespace[];
  workloads: KubeWorkload[];
  selectedNamespace: string;
  loading: boolean;
  error: string | null;
  /** True when the backend returned 503 — cluster not configured or unreachable. */
  clusterUnavailable: boolean;

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
  clusterUnavailable: false,

  fetchNamespaces: async () => {
    set({ loading: true });
    try {
      const namespaces = await kubernetesApi.listNamespaces();
      set({ namespaces, loading: false, clusterUnavailable: false });
    } catch (e) {
      set(applyFetchError(e));
    }
  },

  fetchWorkloads: async (namespace?: string) => {
    set({ loading: true });
    try {
      const ns = namespace || get().selectedNamespace;
      const workloads = await kubernetesApi.listWorkloads(ns);
      set({ workloads, loading: false, clusterUnavailable: false });
    } catch (e) {
      set(applyFetchError(e));
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
