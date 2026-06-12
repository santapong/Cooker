import { useEffect, useMemo } from 'react'
import { useCloudStore } from '../stores/cloudStore'
import { useTheme } from '../theme/ThemeProvider'
import { hexA } from '../theme/tokens'
import { Btn, Card, EmptyState, PageHeader, Pill, Select, StatusDot, statusTone } from '../components/ui/atoms'
import { DataTable } from '../components/ui/DataTable'
import type { CostSummary } from '../types/cloud'
import {
  filterResources,
  flattenResources,
  providerErrors,
  providerLabel,
  resourceTypeLabel,
} from '../utils/cloud'

// ProviderErrorBanner renders one failed provider's message. Mirrors the
// KubernetesPage ClusterUnavailableBanner so partial failures read
// consistently across the app.
function ProviderErrorBanner({ provider, message }: { provider: string; message: string }) {
  const t = useTheme()
  return (
    <div
      role="status"
      aria-live="polite"
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 12,
        padding: '12px 16px',
        marginBottom: 12,
        background: hexA(t.warn, 0.1),
        border: `1px solid ${hexA(t.warn, 0.35)}`,
        borderRadius: 10,
      }}
    >
      <span style={{ flexShrink: 0, width: 8, height: 8, marginTop: 5, borderRadius: 999, background: t.warn }} />
      <div>
        <div style={{ fontFamily: t.sans, fontWeight: 600, fontSize: 13, color: t.text }}>
          {provider.toUpperCase()} could not be queried
        </div>
        <div style={{ fontFamily: t.mono, fontSize: 12, color: t.textSoft, marginTop: 4, lineHeight: 1.5 }}>
          {message}
        </div>
      </div>
    </div>
  )
}

// CostPanel shows month-to-date spend per provider, broken down by
// service. A provider with no cost data (e.g. GCP, whose spend lives in
// the BigQuery export) renders a clear note rather than a fake figure.
function CostPanel({ costs }: { costs: { provider: string; cost?: CostSummary; error?: string }[] }) {
  const t = useTheme()
  if (costs.length === 0) return null
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 14, marginBottom: 22 }}>
      {costs.map(({ provider, cost, error }) => (
        <Card key={provider} pad={16}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 10 }}>
            <span style={{ fontFamily: t.display, fontSize: 15, fontWeight: 600, color: t.text }}>
              {provider.toUpperCase()}
            </span>
            {cost && (
              <span style={{ fontFamily: t.mono, fontSize: 16, fontWeight: 700, color: t.text }}>
                {cost.currency} {cost.total}
              </span>
            )}
          </div>
          {error ? (
            <div style={{ fontFamily: t.mono, fontSize: 11.5, color: t.warn }}>cost unavailable: {error}</div>
          ) : cost && cost.services.length > 0 ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
              {cost.services.slice(0, 6).map((s) => (
                <div key={s.service} style={{ display: 'flex', justifyContent: 'space-between', gap: 10 }}>
                  <span style={{ fontSize: 12, color: t.textSoft, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {s.service}
                  </span>
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.text, flexShrink: 0 }}>
                    {s.amount}
                  </span>
                </div>
              ))}
              <div style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute, marginTop: 6 }}>
                MTD · {cost.start} → {cost.end}
              </div>
            </div>
          ) : (
            <div style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textMute, lineHeight: 1.5 }}>
              No per-service breakdown available for this provider.
            </div>
          )}
        </Card>
      ))}
    </div>
  )
}

export default function CloudPage() {
  const t = useTheme()
  const inventory = useCloudStore((s) => s.inventory)
  const loading = useCloudStore((s) => s.loading)
  const refreshing = useCloudStore((s) => s.refreshing)
  const error = useCloudStore((s) => s.error)
  const providerFilter = useCloudStore((s) => s.providerFilter)
  const typeFilter = useCloudStore((s) => s.typeFilter)
  const fetch = useCloudStore((s) => s.fetch)
  const refresh = useCloudStore((s) => s.refresh)
  const setProviderFilter = useCloudStore((s) => s.setProviderFilter)
  const setTypeFilter = useCloudStore((s) => s.setTypeFilter)

  useEffect(() => {
    fetch()
  }, [fetch])

  const allResources = useMemo(() => flattenResources(inventory), [inventory])
  const filtered = useMemo(
    () => filterResources(allResources, providerFilter, typeFilter),
    [allResources, providerFilter, typeFilter],
  )
  const errors = useMemo(() => providerErrors(inventory), [inventory])
  const costs = useMemo(() => inventory?.results.map((r) => ({ provider: r.provider, cost: r.cost, error: r.error })) ?? [], [inventory])

  const enabled = inventory?.enabled ?? false

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="cloud view"
        title="Cloud inventory"
        subtitle="Read-only inventory and month-to-date spend across the AWS and GCP accounts this server is configured for."
        actions={
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {enabled && (
              <>
                <Select value={providerFilter} onChange={(e) => setProviderFilter(e.target.value as typeof providerFilter)} style={{ width: 140 }}>
                  <option value="all">All providers</option>
                  <option value="aws">AWS</option>
                  <option value="gcp">GCP</option>
                </Select>
                <Select value={typeFilter} onChange={(e) => setTypeFilter(e.target.value as typeof typeFilter)} style={{ width: 160 }}>
                  <option value="all">All resources</option>
                  <option value="compute">Compute</option>
                  <option value="cluster">Kubernetes</option>
                  <option value="registry">Registry</option>
                </Select>
              </>
            )}
            <Btn kind="secondary" onClick={() => refresh()} disabled={refreshing || !enabled}>
              {refreshing ? 'Refreshing…' : 'Refresh'}
            </Btn>
          </div>
        }
      />

      {error && (
        <div
          role="alert"
          style={{
            padding: '12px 16px',
            marginBottom: 16,
            background: hexA(t.bad, 0.1),
            border: `1px solid ${hexA(t.bad, 0.35)}`,
            borderRadius: 10,
            fontFamily: t.mono,
            fontSize: 12.5,
            color: t.bad,
          }}
        >
          {error}
        </div>
      )}

      {/* Not configured: friendly empty state, not an error. */}
      {!loading && !enabled ? (
        <EmptyState
          title="No cloud accounts configured."
          body="Enable a provider to see compute instances, managed Kubernetes clusters, container registries, and month-to-date spend. Set COOKER_CLOUD_AWS_ENABLED (with a region) and/or COOKER_CLOUD_GCP_ENABLED (with a project ID), then restart Cooker."
        />
      ) : (
        <>
          {errors.map((e) => (
            <ProviderErrorBanner key={e.provider} provider={e.provider} message={e.error} />
          ))}

          <CostPanel costs={costs} />

          <Card pad={0}>
            <DataTable
              rows={filtered}
              rowKey={(r) => `${r.provider}-${r.type}-${r.id}`}
              empty={loading ? 'Loading…' : 'No resources match the current filters.'}
              columns={[
                {
                  key: 'name',
                  header: 'Resource',
                  render: (r) => (
                    <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                      <StatusDot tone={statusTone(r.status)} />
                      <div>
                        <div style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}>{r.name}</div>
                        <div style={{ fontSize: 11, color: t.textMute, marginTop: 2 }}>{r.id}</div>
                      </div>
                    </div>
                  ),
                },
                {
                  key: 'provider',
                  header: 'Provider',
                  width: '120px',
                  render: (r) => <Pill>{providerLabel(r.provider)}</Pill>,
                },
                {
                  key: 'type',
                  header: 'Type',
                  width: '140px',
                  render: (r) => <Pill>{resourceTypeLabel(r.type)}</Pill>,
                },
                {
                  key: 'region',
                  header: 'Region',
                  width: '170px',
                  render: (r) => <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>{r.region}</span>,
                },
                {
                  key: 'status',
                  header: 'Status',
                  width: '140px',
                  render: (r) => <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>{r.status}</span>,
                },
              ]}
            />
          </Card>
        </>
      )}
    </div>
  )
}
