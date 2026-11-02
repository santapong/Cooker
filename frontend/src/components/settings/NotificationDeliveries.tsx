import { useEffect, useState, type FormEvent } from 'react';
import { ApiError } from '../../api/client';
import { notificationDeliveriesApi, type NotificationDeliveryPage } from '../../api/notificationDeliveries';
import { useAuth } from '../../auth/OIDCProvider';
import { Actions, Field, FormError, Select, TextInput } from '../ui/form';
import './notification-deliveries.css';

const pageSize = 25;
const states: Record<string, string> = {
  pending: 'Pending', sending: 'Sending', retry_wait: 'Retry scheduled', delivered: 'Receiver accepted',
  failed: 'Failed', cancelled: 'Cancelled', needs_attention: 'Needs attention',
};
const events: Record<string, string> = {
  'run.succeeded': 'Run succeeded', 'run.failed': 'Run failed', 'run.cancelled': 'Run cancelled',
  'deploy.succeeded': 'Deployment succeeded', 'deploy.failed': 'Deployment failed', 'deploy.cancelled': 'Deployment cancelled',
  'build.failed': 'Build failed', 'canary.promoted': 'Canary promoted', 'canary.aborted': 'Canary aborted', 'canary.failed': 'Canary failed',
};
const channels: Record<string, string> = { slack: 'Slack', discord: 'Discord', webhook: 'Webhook', email: 'Email' };
const reasons: Record<string, string> = {
  invalid_configuration: 'The notification target configuration is invalid.',
  receiver_http_rejected: 'The receiver rejected the request.',
  send_outcome_unknown: 'Cooker could not confirm whether the receiver accepted the message.',
  adapter_unavailable: 'This notification channel is unavailable.',
  target_read_unavailable: 'Cooker could not read the target configuration.',
  target_changed: 'The target changed after this delivery was accepted.',
  target_removed: 'The target was removed.',
  target_disabled: 'The target is disabled.',
  worker_lost: 'The sender stopped before its result was recorded.',
};

function timestamp(value: string): string {
  const date = new Date(value);
  return Number.isFinite(date.getTime()) ? date.toLocaleString() : 'Not recorded';
}

export default function NotificationDeliveries() {
  const admin = useAuth().user?.roles.includes('admin') ?? false;
  const [state, setState] = useState('');
  const [targetId, setTargetId] = useState('');
  const [runId, setRunId] = useState('');
  const [filters, setFilters] = useState({ state: '', targetId: '', runId: '' });
  const [offset, setOffset] = useState(0);
  const [revision, setRevision] = useState(0);
  const [page, setPage] = useState<NotificationDeliveryPage | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [observedAt, setObservedAt] = useState('');

  useEffect(() => {
    if (!admin) return;
    let active = true;
    setLoading(true); setPage(null); setError('');
    void notificationDeliveriesApi.list({ ...filters, limit: pageSize, offset })
      .then((result) => {
        if (!active) return;
        if (offset > 0 && offset >= result.total) {
          setOffset(Math.max(0, Math.floor((result.total - 1) / pageSize) * pageSize));
          return;
        }
        setPage(result); setObservedAt(new Date().toISOString());
      })
      .catch((e: unknown) => {
        if (active) setError(e instanceof ApiError && e.status === 403
          ? 'Administrator access and the configured MFA policy are required to read delivery history.'
          : 'Could not read durable notification history. PostgreSQL and administrator access are required. Refresh to try again.');
      })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [admin, filters, offset, revision]);

  if (!admin) return <p>Notification delivery history is available to administrators.</p>;
  function applyFilters(e: FormEvent) {
    e.preventDefault(); setFilters({ state, targetId: targetId.trim(), runId: runId.trim() }); setOffset(0);
  }

  return <div className="notification-history" role="region" aria-label="Notification delivery history">
    <p>Inspect messages for recorded deployment and run events. Receiver accepted means the destination accepted the request; it does not prove a person read it or change the deployment result.</p>
    <details>
      <summary>How retries and recovery work</summary>
      <p>Transient HTTP rejection can retry after 30 seconds, then 2 minutes, up to 3 attempts. Retries can duplicate messages. Uncertain sends require inspection and are not automatically resent.</p>
      <p>Accepted events are retained. Native terminal jobs repair missed event acceptance; uncommitted inline and canary events have no replay source. Changing a target cannot retract a message already in flight.</p>
    </details>
    <form onSubmit={applyFilters} aria-label="Filter notification deliveries">
      <Field label="Notification delivery state"><Select value={state} onChange={(e) => setState(e.target.value)} options={[{ value: '', label: 'All delivery states' }, ...Object.entries(states).map(([value, label]) => ({ value, label }))]} /></Field>
      <Field label="Notification target ID" hint="Optional exact target ID; target credentials are never shown here."><TextInput maxLength={200} value={targetId} onChange={(e) => setTargetId(e.target.value)} /></Field>
      <Field label="Notification run ID" hint="Optional exact Cooker run ID."><TextInput maxLength={200} value={runId} onChange={(e) => setRunId(e.target.value)} /></Field>
      <Actions>
        <button className="hud-btn" disabled={loading}>Apply delivery filters</button>
        <button type="button" className="hud-btn" disabled={loading} onClick={() => setRevision((value) => value + 1)}>Refresh deliveries</button>
      </Actions>
    </form>
    <FormError>{error}</FormError>
    {loading && <p role="status">Loading notification deliveries…</p>}
    {page && <>
      <p>Observed {timestamp(observedAt)}. Refresh to check for newer results.</p>
      <p>{page.pendingSourceJobs} native terminal jobs awaiting event capture · {page.pendingSourceRepairJobs} source repairs waiting.</p>
      {page.pendingSourceRepairJobs > 0 && <p className="notification-warning">Some source events need repair before their deliveries can appear. This list is not a complete receipt of all execution events.</p>}
      <p role="status">{page.total ? `${offset + 1}–${Math.min(offset + page.items.length, page.total)} of ${page.total} deliveries` : 'No matching notification deliveries.'}</p>
      {page.items.map((item) => <article className="notification-delivery" key={item.id} aria-label={`${states[item.state] ?? 'Unknown delivery state'} notification`}>
        <p><strong>{states[item.state] ?? 'Unknown delivery state'}</strong> · {channels[item.kind] ?? 'Unknown channel'} · {events[item.eventType] ?? 'Recorded event'}</p>
        <p>Attempts {item.attempts} of {item.maxAttempts} · last recorded {timestamp(item.updatedAt)}</p>
        {(item.state === 'pending' || item.state === 'retry_wait') && <p>Next scheduled attempt {timestamp(item.nextAttemptAt)}</p>}
        {item.state === 'needs_attention' && <p>Inspect the destination and sender before taking action. Cooker will not automatically resend this message.</p>}
        {item.duplicatePossible && <p className="notification-warning">A duplicate message is possible.</p>}
        {item.lastCode && <p>{reasons[item.lastCode] ?? 'A delivery issue was recorded. Inspect the sender and destination.'}{item.lastHttpStatus ? ` HTTP ${item.lastHttpStatus}.` : ''}</p>}
        <details><summary>Inspect delivery identity</summary>
          <p>Delivery <code>{item.id}</code></p><p>Event <code>{item.eventId}</code></p>
          <p>Target <code>{item.targetId}</code> · accepted version {item.targetVersion}</p>
          <p>Recorded {timestamp(item.createdAt)}</p>
        </details>
      </article>)}
      <Actions>
        <button type="button" className="hud-btn" disabled={loading || offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))}>Previous delivery page</button>
        <button type="button" className="hud-btn" disabled={loading || offset + pageSize >= page.total} onClick={() => setOffset(offset + pageSize)}>Next delivery page</button>
      </Actions>
    </>}
  </div>;
}
