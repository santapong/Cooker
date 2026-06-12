// Cloud inventory & cost panel types (OR-2). These mirror the backend
// model in backend/internal/model/cloudinventory.go.

export type CloudProvider = 'aws' | 'gcp'

export type CloudResourceType = 'compute' | 'cluster' | 'registry'

export interface CloudResource {
  provider: CloudProvider
  type: CloudResourceType
  id: string
  name: string
  region: string
  status: string
  tags?: Record<string, string>
}

export interface CostServiceLine {
  service: string
  amount: string
  currency: string
}

export interface CostSummary {
  provider: CloudProvider
  currency: string
  total: string
  start: string
  end: string
  services: CostServiceLine[]
}

export interface ProviderInventory {
  provider: CloudProvider
  resources: CloudResource[]
  cost?: CostSummary
  error?: string
}

export interface CloudInventory {
  enabled: boolean
  providers: CloudProvider[]
  results: ProviderInventory[]
  fetchedAt: string
}

// GET /cloud/costs projects the cost portion of the inventory.
export interface CloudProviderCost {
  provider: CloudProvider
  cost?: CostSummary
  error?: string
}

export interface CloudCosts {
  enabled: boolean
  providers: CloudProvider[]
  costs: CloudProviderCost[]
  fetchedAt: string
}
