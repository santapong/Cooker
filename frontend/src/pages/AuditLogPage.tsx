import { useCallback, useEffect, useState } from 'react';
import { auditApi, type AuditEvent, type AuditEventFilters } from '../api/admin';
import { useTheme } from '../theme/ThemeProvider';
import { Btn, Card, EmptyState, Input, Label, PageHeader, Pill, Select, type Tone } from '../components/ui/atoms';
import { DataTable } from '../components/ui/DataTable';
import { useToastStore } from '../stores/toastStore';

const PAGE_SIZE = 50;

function statusTone(status: number): Tone {
  if (status >= 500) return 'bad';
  if (status >= 400) return 'warn';
  return 'good';
}

/** datetime-local input value → RFC3339 (empty stays empty). */
function toRFC3339(local: string): string {
  if (!local) return '';
  const d = new Date(local);
  return Number.isNaN(d.getTime()) ? '' : d.toISOString();
}

export default function AuditLogPage() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);

  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [user, setUser] = useState('');
  const [method, setMethod] = useState('');
  const [path, setPath] = useState('');
  const [offset, setOffset] = useState(0);

  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(
    (nextOffset: number) => {
      setLoading(true);
      const filters: AuditEventFilters = {
        from: toRFC3339(from) || undefined,
        to: toRFC3339(to) || undefined,
        user: user || undefined,
        method: method || undefined,
        path: path || undefined,
        limit: PAGE_SIZE,
        offset: nextOffset,
      };
      auditApi
        .list(filters)
        .then((page) => {
          setEvents(page.events);
          setOffset(page.offset);
        })
        .catch((e) => {
          pushToast({ kind: 'error', message: (e as Error).message });
          setEvents([]);
        })
        .finally(() => setLoading(false));
    },
    [from, to, user, method, path, pushToast],
  );

  useEffect(() => {
    load(0);
    // Load once on mount; subsequent fetches are explicit (Apply/paging).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const reset = () => {
    setFrom('');
    setTo('');
    setUser('');
    setMethod('');
    setPath('');
  };

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="who did what, when"
        title="Audit log"
        subtitle="Every mutating API call with its actor, route, status, and latency. Request bodies are never captured."
      />

      <Card pad={0} style={{ marginBottom: 18 }}>
        <div
          style={{
            padding: '14px 18px',
            display: 'grid',
            gridTemplateColumns: 'repeat(5, minmax(140px, 1fr)) auto',
            gap: 12,
            alignItems: 'end',
          }}
        >
          <div>
            <Label>From</Label>
            <Input type="datetime-local" value={from} onChange={(e) => setFrom(e.target.value)} />
          </div>
          <div>
            <Label>To</Label>
            <Input type="datetime-local" value={to} onChange={(e) => setTo(e.target.value)} />
          </div>
          <div>
            <Label>User (subject)</Label>
            <Input value={user} onChange={(e) => setUser(e.target.value)} placeholder="oidc-sub" />
          </div>
          <div>
            <Label>Method</Label>
            <Select value={method} onChange={(e) => setMethod(e.target.value)}>
              <option value="">Any</option>
              {['POST', 'PUT', 'PATCH', 'DELETE'].map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </Select>
          </div>
          <div>
            <Label>Path prefix</Label>
            <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/api/v1/apps" />
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <Btn kind="primary" onClick={() => load(0)} disabled={loading}>
              Apply
            </Btn>
            <Btn kind="ghost" onClick={reset} disabled={loading}>
              Clear
            </Btn>
          </div>
        </div>
      </Card>

      <Card pad={0}>
        {loading ? (
          <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
        ) : events.length === 0 ? (
          <EmptyState
            title="No audit events match."
            body="Events appear here as authenticated users make mutating API calls. Widen the filters or check that auditing is enabled (COOKER_AUDIT_ENABLED) with a db destination."
          />
        ) : (
          <DataTable
            rows={events}
            rowKey={(e) => String(e.id)}
            columns={[
              {
                key: 'time',
                header: 'Time',
                width: '190px',
                render: (e) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                    {new Date(e.time).toLocaleString()}
                  </span>
                ),
              },
              {
                key: 'user',
                header: 'User',
                render: (e) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.text }}>
                    {e.userEmail || e.userSub || '—'}
                  </span>
                ),
              },
              {
                key: 'method',
                header: 'Method',
                width: '90px',
                render: (e) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.text, fontWeight: 600 }}>
                    {e.method}
                  </span>
                ),
              },
              {
                key: 'path',
                header: 'Path',
                render: (e) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                    {e.path}
                  </span>
                ),
              },
              {
                key: 'status',
                header: 'Status',
                width: '90px',
                render: (e) => <Pill tone={statusTone(e.status)}>{e.status}</Pill>,
              },
              {
                key: 'latency',
                header: 'Latency',
                width: '90px',
                render: (e) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textMute }}>
                    {e.latencyMs} ms
                  </span>
                ),
              },
              {
                key: 'ip',
                header: 'Client IP',
                width: '130px',
                render: (e) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textMute }}>
                    {e.clientIp || '—'}
                  </span>
                ),
              },
            ]}
          />
        )}
        <div
          style={{
            display: 'flex',
            justifyContent: 'flex-end',
            alignItems: 'center',
            gap: 10,
            padding: '12px 18px',
            borderTop: `1px solid ${t.line}`,
          }}
        >
          <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textMute }}>
            {offset + 1}–{offset + events.length}
          </span>
          <Btn kind="ghost" onClick={() => load(Math.max(0, offset - PAGE_SIZE))} disabled={loading || offset === 0}>
            ← Newer
          </Btn>
          <Btn
            kind="ghost"
            onClick={() => load(offset + PAGE_SIZE)}
            disabled={loading || events.length < PAGE_SIZE}
          >
            Older →
          </Btn>
        </div>
      </Card>
    </div>
  );
}
