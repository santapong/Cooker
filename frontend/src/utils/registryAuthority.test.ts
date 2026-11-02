import { expect, it, vi } from 'vitest';
import type { RegistryConfig } from '../types/infra';
import { readRegistryGrantTargets, registryGrantState, registrySelectionState } from './registryAuthority';

const registry: RegistryConfig = { id: 'r', name: 'Owned registry', url: 'ghcr.io', version: 3, allowedAppIds: ['app'], allowedPipelineIds: ['pipeline'] };

it('binds app/pipeline grants independently and never authorizes imported or unsaved subjects', () => {
  expect(registryGrantState(registry, { kind: 'app', id: 'app' })).toBe('allowed');
  expect(registryGrantState(registry, { kind: 'pipeline', id: 'app' })).toBe('denied');
  expect(registryGrantState(registry, { kind: 'pipeline', id: 'pipeline' })).toBe('allowed');
  for (const id of [undefined, 'vapp_imported', 'vflow_imported']) expect(registryGrantState(registry, { kind: 'app', id })).toBe('unknown');
});

it('distinguishes authoritative omitted empty grants from unavailable old/malformed metadata', () => {
  expect(registryGrantState({ ...registry, allowedAppIds: undefined }, { kind: 'app', id: 'app' })).toBe('denied');
  for (const version of [undefined, 0, -1, Number.MAX_SAFE_INTEGER + 1, NaN]) expect(registryGrantState({ ...registry, version }, { kind: 'app', id: 'app' })).toBe('unknown');
  expect(registryGrantState({ ...registry, allowedAppIds: [null] } as unknown as RegistryConfig, { kind: 'app', id: 'app' })).toBe('unknown');
});

it('retains revoked and missing selections without inventing anonymous authority or mutating inputs', () => {
  const selected = ['r', 'missing', 'denied'];
  const inventory = [registry, { ...registry, id: 'denied', allowedAppIds: [] }];
  const state = registrySelectionState(inventory, { kind: 'app', id: 'app' }, selected);
  expect(state.available.map((r) => r.id)).toEqual(['r']);
  expect(state.unavailable.map((r) => [r.id, r.state])).toEqual([['missing', 'unknown'], ['denied', 'denied']]);
  expect(state.valid).toBe(false);
  expect(selected).toEqual(['r', 'missing', 'denied']);
  expect(registrySelectionState(inventory, { kind: 'app', id: 'app' }, []).valid).toBe(true);
  expect(registrySelectionState(inventory, { kind: 'app', id: 'app' }, ['r', 'r']).valid).toBe(false);
});

it('reads all pages before claiming target completeness and excludes imported catalog identities', async () => {
  const rows = Array.from({ length: 205 }, (_, n) => ({ id: `app-${n}`, name: `App ${n}` }));
  rows[204] = { id: 'vapp_imported', name: 'Vercel imported' };
  const read = vi.fn(async ({ limit, offset }: { limit: number; offset: number }) => rows.slice(offset, offset + limit));
  const result = await readRegistryGrantTargets(read);
  expect(result.complete).toBe(true); expect(result.items).toHaveLength(204);
  expect(read.mock.calls.map(([q]) => q.offset)).toEqual([0, 100, 200]);
});

it('reports bounded or nonprogressing target reads as incomplete', async () => {
  const page = Array.from({ length: 100 }, (_, n) => ({ id: `p-${n}`, name: `Pipeline ${n}` }));
  const repeated = await readRegistryGrantTargets(async () => page);
  expect(repeated.complete).toBe(false); expect(repeated.items).toHaveLength(100);
  const capped = await readRegistryGrantTargets(async ({ offset }) => page.map((row) => ({ ...row, id: `${offset}-${row.id}` })));
  expect(capped.complete).toBe(false); expect(capped.items).toHaveLength(1000);
});

it('does not turn failed or malformed inventory into a complete empty list', async () => {
  await expect(readRegistryGrantTargets(async () => { throw new Error('owned failure'); })).rejects.toThrow();
  await expect(readRegistryGrantTargets(async () => null as unknown as [])).rejects.toThrow('unavailable');
});
