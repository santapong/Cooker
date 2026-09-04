import { create } from 'zustand';
import type { ComposeGraph, ComposeServicePatch } from '../types/compose';
import { dockerApi } from '../api/docker';

interface ComposeStore {
  graph: ComposeGraph | null;
  /** The file the loaded graph came from (relative to the server's compose dir). */
  composePath: string;
  selectedServiceName: string | null;
  loading: boolean;
  error: string | null;

  fetchComposeGraph: (composePath?: string) => Promise<void>;
  setSelectedService: (name: string | null) => void;
  /** Send the patch to the server; the server rewrites the file and returns the re-parsed graph. Rejects with the server's message. */
  updateServiceConfig: (name: string, patch: ComposeServicePatch) => Promise<{ message: string; service: string }>;
}

export const useComposeStore = create<ComposeStore>((set, get) => ({
  graph: null,
  composePath: 'docker-compose.yml',
  selectedServiceName: null,
  loading: false,
  error: null,

  fetchComposeGraph: async (composePath?: string) => {
    set({ loading: true, error: null });
    try {
      const graph = await dockerApi.parseCompose(composePath);
      set({ graph, composePath: composePath || 'docker-compose.yml', loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  setSelectedService: (name) => {
    set({ selectedServiceName: name });
  },

  updateServiceConfig: async (name, patch) => {
    const res = await dockerApi.updateComposeService(name, patch, get().composePath);
    set((s) => {
      if (res.graph) return { graph: res.graph };
      // Older server without the graph in its reply: mirror the patch locally.
      return s.graph ? { graph: { ...s.graph, services: s.graph.services.map((svc) => (svc.name === name ? { ...svc, ...patch } : svc)) } } : {};
    });
    return res;
  },
}));
