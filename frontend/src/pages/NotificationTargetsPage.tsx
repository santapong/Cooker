import { useEffect, useState } from 'react';
import { notificationTargetsApi, type NotificationTarget } from '../api/admin';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import { timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

export default function NotificationTargetsPage() {
  const [targets, setTargets] = useState<NotificationTarget[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    notificationTargetsApi
      .list()
      .then((list) => {
        if (cancelled) return;
        setTargets(list ?? []);
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

  const rows: ChartRow[] = targets.map((t) => {
    const events = t.eventTypes?.length ?? 0;
    return {
      id: t.id,
      name: t.name,
      sub: t.eventTypes?.join(', '),
      status: t.enabled ? 'ok' : 'idle',
      meta: [t.kind, `${events} ${events === 1 ? 'event' : 'events'}`, t.enabled ? 'enabled' : 'disabled', `updated ${timeAgo(t.updatedAt)}`],
    };
  });

  return (
    <StarChart
      title="Notifications"
      count={targets.length}
      rows={rows}
      hasThumbs={false}
      loading={loading}
      error={error}
      empty={{ text: 'No notification targets. Slack, Discord, email and webhook fan-out appear here.' }}
    />
  );
}
