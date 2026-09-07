import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ComposeGraph } from '../types/compose';
import { COMPOSE_LIBRARY_KEY, useComposeLibraryStore } from './composeLibraryStore';

const graph: ComposeGraph = {
  services: [{ name: 'api', image: 'private/image', environment: { PASSWORD: 'do-not-persist' }, ports: [], dependsOn: [], networks: [], volumes: [], command: '', status: '' }],
  connections: [], networks: ['default'], volumes: [],
};

describe('Compose library persistence', () => {
  let storage: Map<string, string>;
  beforeEach(() => {
    storage = new Map();
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
    });
    useComposeLibraryStore.setState({ stacks: [], error: null });
  });

  it('survives a fresh hydration and persists only references and counts', () => {
    expect(useComposeLibraryStore.getState().saveStack('Production', 'prod.yml', graph)).toBe(true);
    const raw = storage.get(COMPOSE_LIBRARY_KEY)!;
    expect(raw).not.toMatch(/PASSWORD|do-not-persist|private\/image|environment/);
    useComposeLibraryStore.setState({ stacks: [] });
    useComposeLibraryStore.getState().hydrate();
    expect(useComposeLibraryStore.getState().stacks).toEqual([
      { name: 'Production', composePath: 'prod.yml', services: 1, networks: 1, volumes: 0, checkedAt: expect.any(String) },
    ]);
  });

  it('renames the same file without duplicating it and forgets only that reference', () => {
    const s = useComposeLibraryStore.getState();
    s.saveStack('Production', 'prod.yml', graph);
    s.saveStack('Staging', 'staging.yml', graph);
    s.saveStack('Production API', 'prod.yml', graph);
    expect(useComposeLibraryStore.getState().stacks.map((s) => s.name)).toEqual(['Production API', 'Staging']);
    s.forgetStack('prod.yml');
    s.hydrate();
    expect(useComposeLibraryStore.getState().stacks.map((s) => s.composePath)).toEqual(['staging.yml']);
  });

  it('reports failed writes without pretending an entry was saved or removed', () => {
    const s = useComposeLibraryStore.getState();
    s.saveStack('Production', 'prod.yml', graph);
    vi.stubGlobal('localStorage', { setItem: () => { throw new Error('quota'); } });
    expect(s.saveStack('Staging', 'staging.yml', graph)).toBe(false);
    expect(s.forgetStack('prod.yml')).toBe(false);
    expect(useComposeLibraryStore.getState().stacks).toHaveLength(1);
    expect(useComposeLibraryStore.getState().error).toContain('could not be saved');
  });

  it.each(['not json', '[null]', '[{"name":"bad"}]'])('handles corrupt browser storage: %s', (raw) => {
    storage.set(COMPOSE_LIBRARY_KEY, raw);
    useComposeLibraryStore.getState().hydrate();
    expect(useComposeLibraryStore.getState().stacks).toEqual([]);
    expect(useComposeLibraryStore.getState().error).toContain('Could not read');
  });
});
