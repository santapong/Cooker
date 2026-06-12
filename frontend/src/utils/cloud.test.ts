import { describe, it, expect } from 'vitest'
import {
  flattenResources,
  filterResources,
  providerErrors,
  resourceCounts,
  providerLabel,
  resourceTypeLabel,
} from './cloud'
import type { CloudInventory, CloudResource } from '../types/cloud'

const inv: CloudInventory = {
  enabled: true,
  providers: ['aws', 'gcp'],
  fetchedAt: '2026-06-12T00:00:00Z',
  results: [
    {
      provider: 'aws',
      resources: [
        { provider: 'aws', type: 'compute', id: 'i-1', name: 'web', region: 'us-east-1', status: 'running' },
        { provider: 'aws', type: 'registry', id: 'ecr/cooker', name: 'cooker', region: 'us-east-1', status: 'available' },
      ],
      cost: { provider: 'aws', currency: 'USD', total: '10.00', start: '2026-06-01', end: '2026-06-12', services: [] },
    },
    {
      provider: 'gcp',
      resources: [],
      error: 'gcp: compute: permission denied',
    },
  ],
}

describe('flattenResources', () => {
  it('concatenates resources across providers', () => {
    expect(flattenResources(inv)).toHaveLength(2)
  })
  it('returns [] for null', () => {
    expect(flattenResources(null)).toEqual([])
  })
  it('skips providers with no resources (errored)', () => {
    const all = flattenResources(inv)
    expect(all.every((r) => r.provider === 'aws')).toBe(true)
  })
})

describe('filterResources', () => {
  const resources = flattenResources(inv)
  it('returns all when both filters are "all"', () => {
    expect(filterResources(resources, 'all', 'all')).toHaveLength(2)
  })
  it('filters by provider', () => {
    expect(filterResources(resources, 'gcp', 'all')).toHaveLength(0)
    expect(filterResources(resources, 'aws', 'all')).toHaveLength(2)
  })
  it('filters by type', () => {
    const computeOnly = filterResources(resources, 'all', 'compute')
    expect(computeOnly).toHaveLength(1)
    expect(computeOnly[0].id).toBe('i-1')
  })
  it('combines provider and type filters', () => {
    expect(filterResources(resources, 'aws', 'registry')).toHaveLength(1)
    expect(filterResources(resources, 'aws', 'cluster')).toHaveLength(0)
  })
})

describe('providerErrors', () => {
  it('returns only the providers that errored', () => {
    const errs = providerErrors(inv)
    expect(errs).toHaveLength(1)
    expect(errs[0].provider).toBe('gcp')
    expect(errs[0].error).toContain('permission denied')
  })
  it('returns [] for null', () => {
    expect(providerErrors(null)).toEqual([])
  })
})

describe('resourceCounts', () => {
  it('tallies by type with zeros for absent types', () => {
    const counts = resourceCounts(flattenResources(inv))
    expect(counts).toEqual({ compute: 1, cluster: 0, registry: 1 })
  })
  it('handles an empty list', () => {
    expect(resourceCounts([])).toEqual({ compute: 0, cluster: 0, registry: 0 })
  })
})

describe('labels', () => {
  it('renders provider labels', () => {
    expect(providerLabel('aws')).toBe('AWS')
    expect(providerLabel('gcp')).toBe('GCP')
  })
  it('renders resource type labels', () => {
    expect(resourceTypeLabel('compute')).toBe('Compute')
    expect(resourceTypeLabel('cluster')).toBe('Kubernetes')
    expect(resourceTypeLabel('registry')).toBe('Registry')
  })
})

// A resource missing the optional tags field must still flatten/filter
// cleanly (no runtime access on undefined).
describe('optional fields', () => {
  it('tolerates resources without tags', () => {
    const r: CloudResource = { provider: 'aws', type: 'compute', id: 'i-9', name: 'x', region: 'r', status: 's' }
    expect(filterResources([r], 'aws', 'compute')).toHaveLength(1)
  })
})
