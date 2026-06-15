import { create } from 'zustand'
import { cloudApi } from '../api/cloud'
import type { CloudInventory, CloudProvider, CloudResourceType } from '../types/cloud'

// providerFilter / typeFilter use 'all' as the unfiltered sentinel so
// the Select controls bind to a plain string.
export type ProviderFilter = CloudProvider | 'all'
export type TypeFilter = CloudResourceType | 'all'

interface CloudStore {
  inventory: CloudInventory | null
  loading: boolean
  error: string | null
  /** True while a manual Refresh (cache-bust) is in flight. */
  refreshing: boolean
  providerFilter: ProviderFilter
  typeFilter: TypeFilter

  fetch: () => Promise<void>
  refresh: () => Promise<void>
  setProviderFilter: (p: ProviderFilter) => void
  setTypeFilter: (t: TypeFilter) => void
}

export const useCloudStore = create<CloudStore>((set) => ({
  inventory: null,
  loading: false,
  error: null,
  refreshing: false,
  providerFilter: 'all',
  typeFilter: 'all',

  fetch: async () => {
    set({ loading: true, error: null })
    try {
      const inventory = await cloudApi.getInventory()
      set({ inventory, loading: false })
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e), loading: false })
    }
  },

  refresh: async () => {
    set({ refreshing: true, error: null })
    try {
      const inventory = await cloudApi.refresh()
      set({ inventory, refreshing: false })
    } catch (e) {
      set({ error: e instanceof Error ? e.message : String(e), refreshing: false })
    }
  },

  setProviderFilter: (providerFilter) => set({ providerFilter }),
  setTypeFilter: (typeFilter) => set({ typeFilter }),
}))
