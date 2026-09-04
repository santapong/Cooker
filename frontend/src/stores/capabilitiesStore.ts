import { create } from 'zustand';
import { capabilitiesApi, type Capabilities } from '../api/capabilities';

/**
 * Server-declared optional capabilities (feature flags the operator enabled).
 * Fetched once per session by the shell's TopStrip and rendered as badges.
 * Not persisted — server truth, same reasoning as licenseStore.
 */
interface CapabilitiesStore {
  capabilities: Capabilities | null;
  loading: boolean;
  error: string | null;
  fetch: () => Promise<void>;
}

export const useCapabilitiesStore = create<CapabilitiesStore>((set, get) => ({
  capabilities: null,
  loading: false,
  error: null,

  fetch: async () => {
    if (get().loading || get().capabilities) return;
    set({ loading: true, error: null });
    try {
      const capabilities = await capabilitiesApi.get();
      set({ capabilities, loading: false });
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e), loading: false });
    }
  },
}));
