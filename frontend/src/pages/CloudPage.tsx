import { useEffect } from 'react';
import { useCloudStore, type ProviderFilter, type TypeFilter } from '../stores/cloudStore';
import type { CloudResource } from '../types/cloud';
import Panel from '../components/ui/Panel';
import Gauge from '../components/instruments/Gauge';
import { Actions } from '../components/ui/form';
import { ChartRows, type ChartRow } from '../components/list/StarChart';
import { timeAgo } from '../utils/time';

const PROVIDERS: ProviderFilter[] = ['all', 'aws', 'gcp'];
const TYPES: TypeFilter[] = ['all', 'compute', 'cluster', 'registry'];

function resourceStatus(r: CloudResource): 'ok' | 'idle' | 'warn' {
  const s = r.status.toLowerCase();
  if (['running', 'active', 'ready', 'available'].some((k) => s.includes(k))) return 'ok';
  if (['stopped', 'terminated', 'error', 'failed'].some((k) => s.includes(k))) return 'warn';
  return 'idle';
}

/** Cloud — read-only inventory and month-to-date spend across configured AWS / GCP accounts. */
export default function CloudPage() {
  const inventory = useCloudStore((s) => s.inventory);
  const loading = useCloudStore((s) => s.loading);
  const refreshing = useCloudStore((s) => s.refreshing);
  const error = useCloudStore((s) => s.error);
  const providerFilter = useCloudStore((s) => s.providerFilter);
  const typeFilter = useCloudStore((s) => s.typeFilter);
  const fetch = useCloudStore((s) => s.fetch);
  const refresh = useCloudStore((s) => s.refresh);
  const setProviderFilter = useCloudStore((s) => s.setProviderFilter);
  const setTypeFilter = useCloudStore((s) => s.setTypeFilter);

  useEffect(() => {
    void fetch();
  }, [fetch]);

  const results = inventory?.results ?? [];
  const resources = results.flatMap((r) => r.resources).filter((r) => (providerFilter === 'all' || r.provider === providerFilter) && (typeFilter === 'all' || r.type === typeFilter));
  const rows: ChartRow[] = resources.map((r) => ({
    id: `${r.provider}:${r.id}`,
    name: r.name || r.id,
    sub: r.id !== r.name ? r.id : undefined,
    status: resourceStatus(r),
    meta: [r.provider, r.type, r.region, r.status],
  }));

  return (
    <div className="detail">
      <header className="detail-head">
        <div className="grow">
          <h1>Cloud</h1>
          <p>Read-only inventory and month-to-date spend for the accounts this server is configured for.</p>
        </div>
        <Actions>
          <button type="button" className="hud-btn" onClick={() => void refresh()} disabled={refreshing || !inventory?.enabled}>
            {refreshing ? 'Refreshing…' : 'Refresh inventory'}
          </button>
        </Actions>
      </header>
      {error && (
        <div className="form-error" role="alert">
          {error}
        </div>
      )}
      {inventory && !inventory.enabled ? (
        <Panel title="Cloud inventory is off">
          <p>Enable it with COOKER_FEATURE_CLOUD_INVENTORY and configure AWS / GCP credentials; the panels light up on the next fetch.</p>
        </Panel>
      ) : (
        <>
          <div className="gauges">
            {results.map((p) => (
              <Gauge key={p.provider} label={`${p.provider} resources`} value={p.resources.length} sub={p.error ? p.error : p.cost ? `${p.cost.total} ${p.cost.currency} month to date` : undefined} tone={p.error ? 'fail' : 'default'} />
            ))}
            <Gauge label="Fetched" value={inventory?.fetchedAt ? timeAgo(inventory.fetchedAt) : '—'} />
          </div>
          <div className="filters" role="group" aria-label="Filters">
            {PROVIDERS.map((p) => (
              <button key={p} type="button" className="hud-btn" aria-pressed={providerFilter === p} onClick={() => setProviderFilter(p)}>
                {p}
              </button>
            ))}
            <span style={{ width: 8 }} />
            {TYPES.map((t) => (
              <button key={t} type="button" className="hud-btn" aria-pressed={typeFilter === t} onClick={() => setTypeFilter(t)}>
                {t}
              </button>
            ))}
          </div>
          <Panel title="Resources" aside={`${rows.length}`}>
            {rows.length ? <ChartRows rows={rows} hasThumbs={false} /> : <p>{loading ? 'Loading…' : 'No resources match.'}</p>}
          </Panel>
          {results.some((p) => p.cost) && (
            <div className="panel-grid">
              {results.filter((p) => p.cost).map((p) => (
                <Panel key={p.provider} title={`${p.provider} spend`} aside={`${p.cost!.start.slice(0, 10)} → ${p.cost!.end.slice(0, 10)}`}>
                  <div className="kv">
                    {p.cost!.services.map((s) => (
                      <span key={s.service} style={{ display: 'contents' }}>
                        <span className="v muted">{s.service}</span>
                        <span className="v">
                          {s.amount} {s.currency}
                        </span>
                      </span>
                    ))}
                  </div>
                </Panel>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
