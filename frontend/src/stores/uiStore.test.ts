/**
 * uiStore — Calm mode flag + persistence contract.
 * Runs in the default node environment: a minimal in-memory localStorage is
 * installed before the store module (and its persist middleware) loads.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';

function memoryStorage(seed: Record<string, string> = {}) {
  const map = new Map(Object.entries(seed));
  return {
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => void map.set(k, v),
    removeItem: (k: string) => void map.delete(k),
    clear: () => map.clear(),
    key: (i: number) => Array.from(map.keys())[i] ?? null,
    get length() {
      return map.size;
    },
  };
}

describe('uiStore calm mode', () => {
  beforeEach(() => {
    vi.resetModules();
  });

  it('defaults to off and toggles', async () => {
    vi.stubGlobal('localStorage', memoryStorage());
    const { useUIStore } = await import('./uiStore');
    expect(useUIStore.getState().calmMode).toBe(false);
    useUIStore.getState().toggleCalmMode();
    expect(useUIStore.getState().calmMode).toBe(true);
    useUIStore.getState().setCalmMode(false);
    expect(useUIStore.getState().calmMode).toBe(false);
  });

  it('persists calmMode alongside mode and themeMode', async () => {
    const storage = memoryStorage();
    vi.stubGlobal('localStorage', storage);
    const { useUIStore } = await import('./uiStore');
    useUIStore.getState().setCalmMode(true);
    const raw = storage.getItem('cooker-ui');
    expect(raw).not.toBeNull();
    const persisted = JSON.parse(raw as string).state;
    expect(persisted).toEqual({ mode: 'pro', themeMode: 'dark', calmMode: true });
  });

  it('rehydrates a pre-P1 blob without calmMode and keeps the default', async () => {
    const legacy = JSON.stringify({ state: { mode: 'simple', themeMode: 'dark' }, version: 0 });
    vi.stubGlobal('localStorage', memoryStorage({ 'cooker-ui': legacy }));
    const { useUIStore } = await import('./uiStore');
    expect(useUIStore.getState().mode).toBe('simple');
    expect(useUIStore.getState().calmMode).toBe(false);
    expect(typeof useUIStore.getState().toggleCalmMode).toBe('function');
  });
});
