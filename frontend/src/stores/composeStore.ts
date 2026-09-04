import { create } from 'zustand';
import type { ComposeGraph, ComposeServicePatch } from '../types/compose';
import { dockerApi } from '../api/docker';

interface ComposeStore {
  graph: ComposeGraph | null;
  selectedServiceName: string | null;
  loading: boolean;
  error: string | null;

  fetchComposeGraph: (composePath?: string) => Promise<void>;
  setSelectedService: (name: string | null) => void;
  /** Send the patch to the server and mirror it into the loaded graph. Rejects with the server's message. */
  updateServiceConfig: (name: string, patch: ComposeServicePatch) => Promise<{ message: string; service: string }>;
}

export const useComposeStore = create<ComposeStore>((set) => ({
  graph: null,
  selectedServiceName: null,
  loading: false,
  error: null,

  fetchComposeGraph: async (composePath?: string) => {
    set({ loading: true, error: null });
    try {
      const graph = await dockerApi.parseCompose(composePath);
      set({ graph, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  setSelectedService: (name) => {
    set({ selectedServiceName: name });
  },

  updateServiceConfig: async (name, patch) => {
    const res = await dockerApi.updateComposeService(name, patch);
    // The server acknowledges the patch; re-parsing the file would show the
    // pre-edit values, so the loaded graph is patched in place instead.
    set((s) =>
      s.graph ? { graph: { ...s.graph, services: s.graph.services.map((svc) => (svc.name === name ? { ...svc, ...patch } : svc)) } } : {},
    );
    return res;
  },
}));
