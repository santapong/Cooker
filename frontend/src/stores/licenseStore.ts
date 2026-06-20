import { create } from 'zustand';
import { licenseApi, type License, type Entitlements, type LicenseStatus } from '../api/license';

/**
 * The "no license" baseline. The backend returns this shape (status:'none',
 * plan:'free') when nothing is installed and after a DELETE; we keep a local
 * copy so the store always has a coherent `license`/`entitlements` to render
 * even before the first fetch resolves.
 *
 * NOTE: no persist middleware here — entitlements are server truth, fetched
 * on mount. Persisting them would risk showing stale paid features offline,
 * and CLAUDE.md forbids client-side storage of this kind outside auth/.
 */
const FREE_LICENSE: License = {
  status: 'none',
  plan: 'free',
  seats: 0,
  features: [],
  entitlements: { plan: 'free', seats: 0, features: {} },
  customer: '',
  issuedAt: '',
  expiresAt: null,
  installedAt: '',
  installedByEmail: '',
};

interface LicenseStore {
  license: License;
  entitlements: Entitlements;
  status: LicenseStatus;
  loading: boolean;
  /** True while an install/remove mutation is in flight. */
  mutating: boolean;
  error: string | null;

  fetch: () => Promise<void>;
  install: (token: string) => Promise<void>;
  remove: () => Promise<void>;
}

function apply(set: (p: Partial<LicenseStore>) => void, lic: License) {
  set({ license: lic, entitlements: lic.entitlements, status: lic.status });
}

export const useLicenseStore = create<LicenseStore>((set) => ({
  license: FREE_LICENSE,
  entitlements: FREE_LICENSE.entitlements,
  status: 'none',
  loading: false,
  mutating: false,
  error: null,

  fetch: async () => {
    set({ loading: true, error: null });
    try {
      const lic = await licenseApi.get();
      apply(set, lic);
      set({ loading: false });
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e), loading: false });
    }
  },

  install: async (token: string) => {
    set({ mutating: true, error: null });
    try {
      const lic = await licenseApi.install(token);
      apply(set, lic);
      set({ mutating: false });
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      set({ error: message, mutating: false });
      throw e;
    }
  },

  remove: async () => {
    set({ mutating: true, error: null });
    try {
      // The DELETE response is the complete free shape (status:'none',
      // plan:'free', empty entitlements) — trust it directly rather than
      // forcing a local baseline, so the UI reflects the server's truth.
      const reset = await licenseApi.remove();
      apply(set, reset);
      set({ mutating: false });
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      set({ error: message, mutating: false });
      throw e;
    }
  },
}));
