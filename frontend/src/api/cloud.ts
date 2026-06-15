import { get, post } from './client'
import type { CloudCosts, CloudInventory } from '../types/cloud'

export const cloudApi = {
  getInventory: () => get<CloudInventory>('/cloud/inventory'),
  getCosts: () => get<CloudCosts>('/cloud/costs'),
  // POST /cloud/refresh busts the server-side cache and returns the
  // fresh inventory. writeRole + rate-limited on the backend.
  refresh: () => post<CloudInventory>('/cloud/refresh'),
}
