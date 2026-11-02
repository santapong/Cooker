import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { triggerHistoryApi, webhookAttemptScopeQuery, type WebhookAttempt, type WebhookAttemptPage, type WebhookAttemptScope } from '../../api/triggerHistory';
import { Actions, Field, FormError, Select, TextInput } from '../ui/form';

const statuses = [{ value: '', label: 'All request outcomes' }, { value: 'received', label: 'Processing / result unknown' }, { value: 'accepted', label: 'Accepted into inbox' }, { value: 'ignored', label: 'Ignored request type' }, { value: 'duplicate', label: 'Authenticated retry' }, { value: 'rejected', label: 'Rejected request' }, { value: 'failed', label: 'Request processing failed' }, { value: 'uncertain', label: 'Inbox admission unconfirmed' }];
const reasons: Record<string, string> = {
  processing: 'Processing began without a confirmed final audit result. A crash or interrupted response is possible; this does not prove a deployment outcome.',
  admitted: 'The authenticated request was recorded in the inbox. Inspect its rule decisions; it may have created no deployment.',
  already_recorded: 'This authenticated retry reused the original delivery. It did not re-admit a deployment intent.',
  unsupported_event: 'This request type does not trigger deployment.', invalid_delivery_identity: 'The provider delivery identity was missing or invalid; its invalid value was not retained.',
  invalid_json: 'The authenticated request was not valid JSON.', invalid_event: 'The authenticated event did not contain a valid supported source ref and revision.',
  repository_not_allowed: 'The repository was outside this connection’s allowed repositories.', installation_mismatch: 'The GitHub installation did not match the connection.',
  tag_source_unavailable: 'Tag source verification was unavailable.', tag_identity_unverified: 'The tag repository identity could not be verified.',
  tag_revision_unavailable: 'The tag revision could not be verified. Retry using its original provider identity after checking access.', tag_changed: 'The tag changed before acceptance; review its current revision.',
  delivery_identity_conflict: 'The same provider delivery identity was used with different authenticated content; the original inbox record was preserved.',
  history_unavailable: 'The durable inbox could not be read.', admission_unconfirmed: 'Inbox admission could not be confirmed. Check the original delivery before deciding to retry; no rollout outcome is inferred.',
};

export function WebhookAttemptRows({ items, deliveryHref, onInspect }: { items: WebhookAttempt[]; deliveryHref: (id: string) => string; onInspect: () => void }) {
  return <>{items.map((a) => <article key={a.id} className="section" aria-label="Webhook request attempt">
    <p><strong>{statuses.find((s) => s.value === a.status)?.label || 'Outcome unknown'}</strong> · {a.provider}</p>
    <p>{reasons[a.reasonCode] || 'The safe processing explanation is unavailable.'}</p>
    <p>Received {new Date(a.createdAt).toLocaleString()}{a.finishedAt ? ` · audit completed ${new Date(a.finishedAt).toLocaleString()}` : ' · completion unconfirmed'}</p>
    <details><summary>Inspect request identities</summary><p>Attempt <code style={{ overflowWrap: 'anywhere' }}>{a.id}</code></p><p>Connection <code style={{ overflowWrap: 'anywhere' }}>{a.connectionId}</code></p>{a.providerDeliveryId && <p>Provider delivery <code style={{ overflowWrap: 'anywhere' }}>{a.providerDeliveryId}</code></p>}
      {a.deliveryId && <Link className="hud-btn" to={deliveryHref(a.deliveryId)} onClick={onInspect}>Open matching delivery decisions</Link>}
    </details>
  </article>)}</>;
}

export default function WebhookAttemptsPanel({ appId, connectionId, onInspect }: { appId?: string; connectionId?: string; onInspect: () => void }) {
  const [params] = useSearchParams();
  const [scope, setScope] = useState<WebhookAttemptScope>(appId || !connectionId ? 'all' : 'connection'); const [status, setStatus] = useState(''); const [provider, setProvider] = useState(''); const [search, setSearch] = useState('');
  const [offset, setOffset] = useState(0); const [revision, setRevision] = useState(0); const [page, setPage] = useState<WebhookAttemptPage | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true);
  useEffect(() => {
    let active = true; setLoading(true); setError(''); setPage(null);
    void Promise.resolve().then(() => triggerHistoryApi.attempts({ ...webhookAttemptScopeQuery(scope, appId, connectionId), status, provider, q: search, limit: 25, offset })).then((result) => {
      if (!active) return;
      if (result.total > 0 && result.offset >= result.total) { setOffset(Math.floor((result.total - 1) / 25) * 25); return; }
      setPage(result);
    }).catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : 'Cannot load authenticated request attempts'); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [appId, connectionId, scope, status, provider, search, offset, revision]);
  const change = (action: () => void) => { action(); setOffset(0); };
  const link = (id: string) => { const next = new URLSearchParams(params); next.set('gitDelivery', id); return `?${next.toString()}`; };
  return <section className="section" aria-label="Authenticated webhook attempts">
    <h4>Incoming request attempts</h4>
    <p>Requests are verified before they enter this audit. A request’s processing result is separate from deployment success. Each retry has its own record.</p>
    {connectionId && <p>This source connection may serve several pipelines. Requests without a delivery decision are not assigned to this pipeline.</p>}
    <Field label="Request scope" hint={connectionId ? 'Source connection history includes authenticated requests without a target decision.' : 'Unassigned and rejected requests appear under All incoming requests.'}><Select value={scope} onChange={(e) => change(() => setScope(e.target.value as WebhookAttemptScope))} options={[{ value: 'all', label: 'All incoming requests' }, ...(appId ? [{ value: 'app', label: 'Requests linked to this App' }] : []), ...(connectionId ? [{ value: 'connection', label: 'Requests for this source connection' }] : [])]} /></Field>
    <Field label="Request outcome"><Select value={status} onChange={(e) => change(() => setStatus(e.target.value))} options={statuses} /></Field>
    <Field label="Request Git provider"><Select value={provider} onChange={(e) => change(() => setProvider(e.target.value))} options={[{ value: '', label: 'All Git providers' }, { value: 'github', label: 'GitHub' }, { value: 'gitlab', label: 'GitLab' }, { value: 'gitea', label: 'Gitea' }]} /></Field>
    <Field label="Search request identities" hint="Attempt, connection, provider delivery or inbox delivery ID."><TextInput value={search} onChange={(e) => change(() => setSearch(e.target.value))} /></Field>
    <Actions><button className="hud-btn" disabled={loading} onClick={() => setRevision((v) => v + 1)}>Refresh request attempts</button></Actions>
    <FormError>{error}</FormError>{loading && <p role="status">Loading authenticated request attempts…</p>}
    {page && <>
      <p>Audit started {new Date(page.journalStartedAt).toLocaleString()}. Earlier attempts cannot be reconstructed.</p>
      <p role="status">{page.total ? `${page.offset + 1}–${Math.min(page.offset + page.items.length, page.total)} of ${page.total} request attempts` : 'No matching request attempts'}</p>
      <WebhookAttemptRows items={page.items} deliveryHref={link} onInspect={onInspect} />
      <Actions><button className="hud-btn" disabled={loading || offset === 0} onClick={() => setOffset(Math.max(0, offset - 25))}>Previous request attempts</button><button className="hud-btn" disabled={loading || offset + page.items.length >= page.total} onClick={() => setOffset(offset + 25)}>Next request attempts</button></Actions>
      <details><summary>Request audit coverage</summary><ul>{page.limitations.map((text) => <li key={text}>{text}</li>)}</ul></details>
    </>}
  </section>;
}
