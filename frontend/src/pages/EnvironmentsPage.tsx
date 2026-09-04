import { useEffect, useState } from 'react';
import { environmentsApi } from '../api/environments';
import type { Environment } from '../types/environment';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import { timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

export default function EnvironmentsPage() {
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    environmentsApi
      .list({ limit: 100 })
      .then((list) => {
        if (cancelled) return;
        setEnvs([...(list ?? [])].sort((a, b) => a.order - b.order));
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

  const rows: ChartRow[] = envs.map((env) => {
    const vars = Object.keys(env.variables ?? {}).length;
    const promo = env.promotion?.strategy === 'manual' ? `manual · ${env.promotion.requiredApprovers ?? 1} approvers` : 'auto-promote';
    return {
      id: env.id,
      name: env.name,
      sub: env.target ? `${env.target.type} · ${env.target.namespace || env.target.clusterId || '—'}` : undefined,
      status: 'idle',
      meta: [`lane ${env.order}`, promo, `${vars} ${vars === 1 ? 'var' : 'vars'}`, `created ${timeAgo(env.createdAt)}`],
    };
  });

  return (
    <StarChart
      title="Environments"
      count={envs.length}
      rows={rows}
      hasThumbs={false}
      loading={loading}
      error={error}
      empty={{ text: 'No environments yet. Dev, Staging and Production lanes appear here once configured.' }}
    />
  );
}
