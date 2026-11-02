import type { RegistryConfig } from '../types/infra';

export interface RegistrySubject {
  kind: 'app' | 'pipeline';
  id?: string;
}

export type RegistryGrantState = 'allowed' | 'denied' | 'unknown';

export function registryVersionKnown(registry: RegistryConfig): boolean {
  return Number.isSafeInteger(registry.version) && (registry.version ?? 0) > 0;
}

export function registryGrantState(registry: RegistryConfig, subject: RegistrySubject): RegistryGrantState {
  if (!subject.id || subject.id.startsWith('vapp_') || subject.id.startsWith('vflow_') || !registryVersionKnown(registry)) return 'unknown';
  const grants = subject.kind === 'app' ? registry.allowedAppIds : registry.allowedPipelineIds;
  // The current API omits authoritative empty arrays. Only a known generation
  // distinguishes that response from an older backend without grant evidence.
  if (grants === undefined) return 'denied';
  if (!Array.isArray(grants) || grants.some((id) => typeof id !== 'string' || !id)) return 'unknown';
  return grants.includes(subject.id) ? 'allowed' : 'denied';
}

export function registrySelectionState(registries: readonly RegistryConfig[], subject: RegistrySubject, selected: readonly string[]) {
  const byId = new Map(registries.map((registry) => [registry.id, registry]));
  const available = registries.filter((registry) => registryGrantState(registry, subject) === 'allowed');
  const unavailable = selected.flatMap((id) => {
    const registry = byId.get(id);
    const state = registry ? registryGrantState(registry, subject) : 'unknown';
    return state === 'allowed' ? [] : [{ id, name: registry?.name ?? id, state }];
  });
  return { available, unavailable, valid: new Set(selected).size === selected.length && selected.length <= 32 && unavailable.length === 0 };
}

export interface RegistryGrantTarget { id: string; name: string }
export interface RegistryGrantTargets { items: RegistryGrantTarget[]; complete: boolean }

/** Read every bounded page, without treating a truncated/ignored offset as complete. */
export async function readRegistryGrantTargets(read: (page: { limit: number; offset: number }) => Promise<RegistryGrantTarget[]>): Promise<RegistryGrantTargets> {
  const found = new Map<string, RegistryGrantTarget>();
  const limit = 100;
  for (let page = 0; page < 10; page++) {
    const rows = await read({ limit, offset: page * limit });
    if (!Array.isArray(rows) || rows.some((row) => !row || typeof row.id !== 'string' || !row.id || typeof row.name !== 'string')) throw new Error('Registry grant target inventory is unavailable.');
    let added = 0;
    for (const row of rows) {
      if (row.id.startsWith('vapp_') || row.id.startsWith('vflow_')) continue;
      if (!found.has(row.id)) added++;
      found.set(row.id, { id: row.id, name: row.name });
    }
    if (rows.length < limit) return { items: [...found.values()], complete: true };
    if (added === 0) return { items: [...found.values()], complete: false };
  }
  return { items: [...found.values()], complete: false };
}
