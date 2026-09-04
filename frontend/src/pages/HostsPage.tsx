import { useEffect, useState } from 'react';
import { hostsApi } from '../api/hosts';
import type { Host } from '../types/infra';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import { timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

export default function HostsPage() {
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    hostsApi
      .list({ limit: 100 })
      .then((list) => {
        if (cancelled) return;
        setHosts(list ?? []);
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

  const rows: ChartRow[] = hosts.map((h) => ({
    id: h.id,
    name: h.name,
    sub: h.dockerEndpoint ?? h.sshEndpoint ?? h.tailnetIp ?? h.kubeconfigRef,
    status: 'idle',
    meta: [
      h.kind,
      h.reachability,
      ...(h.kind === 'ssh-docker' ? [h.hasSSHPrivateKey ? 'key on file' : 'no key'] : []),
      `updated ${timeAgo(h.updatedAt)}`,
    ],
  }));

  return (
    <StarChart
      title="Hosts"
      count={hosts.length}
      rows={rows}
      hasThumbs={false}
      loading={loading}
      error={error}
      empty={{ text: 'No hosts yet. Docker daemons, SSH hosts and clusters Cooker can deploy to appear here.' }}
    />
  );
}
