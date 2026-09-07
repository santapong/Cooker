import { create } from 'zustand';
import { API_ORIGIN } from '../api/origin';
import type { ComposeGraph } from '../types/compose';

export interface SavedComposeStack {
  name: string;
  composePath: string;
  services: number;
  networks: number;
  volumes: number;
  checkedAt: string;
}

// Browser origins scope same-origin installs; split-origin APIs get their own library.
export const COMPOSE_LIBRARY_KEY = `cooker-compose-library:${API_ORIGIN || 'same-origin'}`;

function isStack(value: unknown): value is SavedComposeStack {
  if (!value || typeof value !== 'object') return false;
  const s = value as Record<string, unknown>;
  return typeof s.name === 'string' && !!s.name.trim()
    && typeof s.composePath === 'string' && !!s.composePath.trim()
    && typeof s.checkedAt === 'string' && Number.isFinite(Date.parse(s.checkedAt))
    && ['services', 'networks', 'volumes'].every((key) => Number.isInteger(s[key]) && Number(s[key]) >= 0);
}

interface ComposeLibraryStore {
  stacks: SavedComposeStack[];
  error: string | null;
  hydrate: () => void;
  saveStack: (name: string, composePath: string, graph: ComposeGraph) => boolean;
  forgetStack: (composePath: string) => boolean;
}

/** Persist references and counts only, never YAML, environment values or graph payloads. */
export const useComposeLibraryStore = create<ComposeLibraryStore>((set, get) => {
  const write = (stacks: SavedComposeStack[]): boolean => {
    try {
      localStorage.setItem(COMPOSE_LIBRARY_KEY, JSON.stringify(stacks));
      set({ stacks, error: null });
      return true;
    } catch {
      set({ error: 'Browser storage is unavailable. Your stack library could not be saved.' });
      return false;
    }
  };
  return {
    stacks: [],
    error: null,
    hydrate: () => {
      try {
        const saved: unknown = JSON.parse(localStorage.getItem(COMPOSE_LIBRARY_KEY) ?? '[]');
        if (!Array.isArray(saved) || !saved.every(isStack)) throw new Error('invalid library');
        const stacks = saved.map(({ name, composePath, services, networks, volumes, checkedAt }) => ({ name, composePath, services, networks, volumes, checkedAt }));
        set({ stacks, error: null });
      } catch {
        set({ error: 'Could not read your saved stacks. You can still open a Compose file.' });
      }
    },
    saveStack: (name, composePath, graph) => {
      if (!name.trim() || !composePath.trim()) return false;
      const stack: SavedComposeStack = {
        name: name.trim(), composePath,
        services: graph.services.length, networks: graph.networks.length, volumes: graph.volumes.length,
        checkedAt: new Date().toISOString(),
      };
      return write([stack, ...get().stacks.filter((s) => s.composePath !== composePath)]);
    },
    forgetStack: (composePath) => write(get().stacks.filter((s) => s.composePath !== composePath)),
  };
});
