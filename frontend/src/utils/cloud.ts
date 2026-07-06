// Pure helpers for the Cloud panel: flatten the per-provider inventory
// into a single resource list, filter it, and surface per-provider
// errors. Kept out of the component so they are unit-testable.

import type {
  CloudInventory,
  CloudProvider,
  CloudResource,
  CloudResourceType,
  ProviderInventory,
} from '../types/cloud'

export type ProviderFilterValue = CloudProvider | 'all'
export type TypeFilterValue = CloudResourceType | 'all'

/** flattenResources concatenates every provider's resources (skipping
 *  providers that errored, which carry no resources). */
export function flattenResources(inv: CloudInventory | null): CloudResource[] {
  if (!inv) return []
  return inv.results.flatMap((r) => r.resources ?? [])
}

/** filterResources applies the provider + type filters ('all' = no
 *  filter). */
export function filterResources(
  resources: CloudResource[],
  provider: ProviderFilterValue,
  type: TypeFilterValue,
): CloudResource[] {
  return resources.filter(
    (r) =>
      (provider === 'all' || r.provider === provider) &&
      (type === 'all' || r.type === type),
  )
}

export interface ProviderError {
  provider: CloudProvider
  error: string
}

/** providerErrors returns the providers that failed, for the per-
 *  provider error banners. */
export function providerErrors(inv: CloudInventory | null): ProviderError[] {
  if (!inv) return []
  return inv.results
    .filter((r): r is ProviderInventory & { error: string } => !!r.error)
    .map((r) => ({ provider: r.provider, error: r.error }))
}

/** resourceCount tallies resources by type for the summary chips. */
export function resourceCounts(
  resources: CloudResource[],
): Record<CloudResourceType, number> {
  const out: Record<CloudResourceType, number> = { compute: 0, cluster: 0, registry: 0 }
  for (const r of resources) {
    out[r.type] = (out[r.type] ?? 0) + 1
  }
  return out
}

/** providerLabel renders a provider id for display. */
export function providerLabel(p: CloudProvider): string {
  return p === 'aws' ? 'AWS' : 'GCP'
}

/** resourceTypeLabel renders a resource type for display. */
export function resourceTypeLabel(t: CloudResourceType): string {
  switch (t) {
    case 'compute':
      return 'Compute'
    case 'cluster':
      return 'Kubernetes'
    case 'registry':
      return 'Registry'
  }
}
