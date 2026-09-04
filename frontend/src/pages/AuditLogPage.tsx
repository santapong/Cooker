import { useCallback, useEffect, useState } from 'react';
import { auditApi, type AuditEvent } from '../api/admin';
import StarChart, { type ChartRow, type ChartStatus } from '../components/list/StarChart';
import { timeAgo } from '../utils/time';

const PAGE = 50;
const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

function codeStatus(code: number): ChartStatus {
  if (code >= 500) return 'fail';
  if (code >= 400) return 'warn';
  if (code >= 200 && code < 300) return 'ok';
  return 'idle';
}

export default function AuditLogPage() {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [offset, setOffset] = useState(0);
  const [exhausted, setExhausted] = useState(false);

  const load = useCallback(async (from: number) => {
    setLoading(true);
    try {
      const page = await auditApi.list({ limit: PAGE, offset: from });
      const got = page?.events ?? [];
      setEvents((prev) => (from === 0 ? got : [...prev, ...got]));
      setExhausted(got.length < PAGE);
      setOffset(from + got.length);
    } catch (e) {
      setError(message(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(0);
  }, [load]);

  const rows: ChartRow[] = events.map((ev) => ({
    id: String(ev.id),
    name: `${ev.method} ${ev.path}`,
    sub: ev.userEmail ?? ev.userSub ?? undefined,
    status: codeStatus(ev.status),
    meta: [String(ev.status), `${ev.latencyMs} ms`, ...(ev.clientIp ? [ev.clientIp] : []), timeAgo(ev.time)],
  }));

  return (
    <StarChart
      title="Audit"
      count={events.length}
      rows={rows}
      hasThumbs={false}
      loading={loading}
      error={error}
      empty={{ text: 'No audit events yet. Enable the db audit destination to query requests here.' }}
      footer={
        !exhausted && events.length > 0 ? (
          <button type="button" className="hud-btn chart-more" onClick={() => void load(offset)} disabled={loading}>
            {loading ? 'Loading…' : 'Load more'}
          </button>
        ) : undefined
      }
    />
  );
}
