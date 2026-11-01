import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ComposeGraph } from '../types/compose';

const parse = vi.fn();
const update = vi.fn();
vi.mock('../api/docker', () => ({ dockerApi: { parseCompose: (...args: unknown[]) => parse(...args), updateComposeService: (...args: unknown[]) => update(...args) } }));
import { useComposeStore } from './composeStore';

const graph = (network: string): ComposeGraph => ({ services: [], connections: [], networks: [network], volumes: [] });
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

describe('Compose file identity', () => {
  beforeEach(() => {
    parse.mockReset(); update.mockReset();
    useComposeStore.setState({ graph: graph('old'), composePath: 'old.yml', selectedServiceName: 'api', loading: false, error: null });
  });

  it('retains the actual file and graph when a different file fails to open', async () => {
    parse.mockRejectedValue(new Error('file missing'));
    expect(await useComposeStore.getState().fetchComposeGraph('missing.yml')).toBe(false);
    expect(useComposeStore.getState()).toMatchObject({ composePath: 'old.yml', graph: graph('old'), error: 'file missing', loading: false });
    update.mockResolvedValue({ message: 'updated', service: 'api' });
    const patch = { image: 'api:2', ports: [], environment: {} };
    await useComposeStore.getState().updateServiceConfig('api', patch);
    expect(update).toHaveBeenCalledWith('api', patch, 'old.yml');
  });

  it('keeps the last requested stack when reads finish out of order', async () => {
    const first = deferred<ComposeGraph>();
    parse.mockReturnValueOnce(first.promise).mockResolvedValueOnce(graph('new'));
    const old = useComposeStore.getState().fetchComposeGraph('slow.yml');
    await useComposeStore.getState().fetchComposeGraph('new.yml');
    first.resolve(graph('slow'));
    expect(await old).toBe(false);
    expect(useComposeStore.getState()).toMatchObject({ composePath: 'new.yml', graph: graph('new'), selectedServiceName: null, loading: false });
  });

  it('does not apply a service response from a previous stack to the current graph', async () => {
    const pending = deferred<{ message: string; service: string; graph: ComposeGraph }>();
    update.mockReturnValue(pending.promise);
    const saved = useComposeStore.getState().updateServiceConfig('api', { image: 'api:2', ports: [], environment: {} });
    parse.mockResolvedValue(graph('new'));
    await useComposeStore.getState().fetchComposeGraph('new.yml');
    pending.resolve({ message: 'updated', service: 'api', graph: graph('old-updated') });
    await saved;
    expect(useComposeStore.getState()).toMatchObject({ composePath: 'new.yml', graph: graph('new') });
  });
});
