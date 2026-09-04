import { useEffect, useState } from 'react';
import { schedulesApi, type Schedule } from '../api/admin';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import { usePortholeTransition } from '../hooks/usePortholeTransition';
import { shortId, timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

export default function SchedulesPage() {
  const open = usePortholeTransition();
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    schedulesApi
      .list()
      .then((list) => {
        if (cancelled) return;
        setSchedules(list ?? []);
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

  const rows: ChartRow[] = schedules.map((s) => {
    const runUrl = s.lastRunId ? `/pipelines/${s.pipelineId}/runs/${s.lastRunId}` : null;
    return {
      id: s.id,
      name: s.name || s.cronExpr,
      sub: `pipeline ${shortId(s.pipelineId)}`,
      status: s.enabled ? 'ok' : 'idle',
      meta: [s.cronExpr, s.timezone, `next ${timeAgo(s.nextRunAt)}`, ...(s.lastRunAt ? [`last ${timeAgo(s.lastRunAt)}`] : [])],
      trailing: runUrl ? (
        <a
          href={runUrl}
          onClick={(ev) => {
            ev.preventDefault();
            open(runUrl, null);
          }}
        >
          last run ↗
        </a>
      ) : undefined,
    };
  });

  return (
    <StarChart
      title="Schedules"
      count={schedules.length}
      rows={rows}
      hasThumbs={false}
      loading={loading}
      error={error}
      empty={{ text: 'No schedules. Cron-triggered runs appear here once the scheduler feature is enabled.' }}
    />
  );
}
