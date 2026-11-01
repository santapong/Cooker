import { create } from 'zustand';
import type { ComposeGraph, ComposeServicePatch } from '../types/compose';
import { dockerApi } from '../api/docker';
import { keepServiceOrder } from '../components/instruments/composeEdit';

interface ComposeStore {
  graph: ComposeGraph | null;
  /** The file the loaded graph came from (relative to the server's compose dir). */
  composePath: string;
  selectedServiceName: string | null;
  loading: boolean;
  error: string | null;

  fetchComposeGraph: (composePath?: string) => Promise<boolean>;
  setSelectedService: (name: string | null) => void;
  /** Send the patch to the server; the server rewrites the file and returns the re-parsed graph. Rejects with the server's message. */
  updateServiceConfig: (name: string, patch: ComposeServicePatch) => Promise<{ message: string; service: string }>;
}

export const useComposeStore = create<ComposeStore>((set, get) => {
  let loadRevision = 0;
  return {
    graph: null,
    composePath: 'docker-compose.yml',
    selectedServiceName: null,
    loading: false,
    error: null,

    fetchComposeGraph: async (composePath?: string) => {
      const revision = ++loadRevision;
      set({ loading: true, error: null });
      try {
        const graph = await dockerApi.parseCompose(composePath);
        if (revision !== loadRevision) return false;
        set({ graph, composePath: composePath || 'docker-compose.yml', loading: false, selectedServiceName: null });
        return true;
      } catch (e) {
        if (revision === loadRevision) set({ error: (e as Error).message, loading: false });
        return false;
      }
    },

    setSelectedService: (name) => {
      set({ selectedServiceName: name });
    },

    updateServiceConfig: async (name, patch) => {
      const revision = loadRevision;
      const res = await dockerApi.updateComposeService(name, patch, get().composePath);
      // Ignore a response for a stack the user has since left.
      if (revision !== loadRevision) return res;
      set((s) => {
        if (res.graph) return { graph: { ...res.graph, services: keepServiceOrder(s.graph?.services, res.graph.services) } };
        // Older server without the graph in its reply: mirror the patch locally.
        return s.graph ? { graph: { ...s.graph, services: s.graph.services.map((svc) => (svc.name === name ? { ...svc, ...patch } : svc)) } } : {};
      });
      return res;
    },
  };
});
