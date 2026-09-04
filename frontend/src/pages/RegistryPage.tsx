import { useEffect, useState } from 'react';
import { registryApi } from '../api/registry';
import { settingsApi } from '../api/settings';
import type { RegistryConfig } from '../types/infra';
import StarChart, { ChartRows, type ChartRow } from '../components/list/StarChart';
import Caps from '../components/ui/Caps';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

export default function RegistryPage() {
  const [repos, setRepos] = useState<string[]>([]);
  const [spec, setSpec] = useState<string | null>(null);
  const [registries, setRegistries] = useState<RegistryConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    Promise.all([
      registryApi.listRepositories() as Promise<{ repositories: string[]; ociSpec?: string }>,
      settingsApi.listRegistries().catch(() => [] as RegistryConfig[]),
    ])
      .then(([r, regs]) => {
        if (cancelled) return;
        setRepos(r?.repositories ?? []);
        setSpec(r?.ociSpec ?? null);
        setRegistries(regs ?? []);
        setLoading(false);
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          setError(message(e));
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const rows: ChartRow[] = repos.map((name) => ({ id: name, name, status: 'idle', meta: ['OCI repository'] }));
  const regRows: ChartRow[] = registries.map((r) => ({
    id: r.id,
    name: r.name,
    sub: r.url,
    status: r.hasPassword ? 'ok' : 'idle',
    meta: [r.username ? `user ${r.username}` : 'anonymous', r.hasPassword ? 'credentials on file' : 'no credentials'],
  }));

  return (
    <StarChart
      title="Registry"
      count={repos.length}
      rows={rows}
      hasThumbs={false}
      loading={loading}
      error={error}
      empty={{ text: 'No repositories yet. Images Cooker pushes land here.' }}
      footer={
        <div className="chart-section">
          <Caps as="h2">Configured registries {spec ? `· ${spec}` : ''}</Caps>
          {regRows.length ? <ChartRows rows={regRows} hasThumbs={false} /> : <p>No external registries configured.</p>}
        </div>
      }
    />
  );
}
