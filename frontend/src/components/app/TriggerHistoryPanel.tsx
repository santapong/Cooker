import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { historyStatuses, triggerHistoryApi, type TriggerHistoryPage } from '../../api/triggerHistory';
import { automationApi } from '../../api/automation';
import { Actions, Field, FormError, Select, TextInput } from '../ui/form';
import WebhookAttemptsPanel from './WebhookAttemptsPanel';

const labels: Record<string, string> = { accepted: 'Accepted', ignored: 'Ignored', duplicate: 'Duplicate', stale: 'Stale', rejected: 'Rejected', review_required: 'Review required', failed: 'Failed', archived: 'Archived', unknown: 'Unknown' };

export default function TriggerHistoryPanel({ appId, pipelineId, sourceConnectionId }: { appId?: string; pipelineId?: string; sourceConnectionId?: string }) {
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedDelivery = searchParams.get('gitDelivery') ?? '';
  const [scope, setScope] = useState('app');
  const [status, setStatus] = useState('');
  const [provider, setProvider] = useState('');
  const [ref, setRef] = useState('');
  const [search, setSearch] = useState('');
  const [offset, setOffset] = useState(0);
  const [revision, setRevision] = useState(0);
  const [page, setPage] = useState<TriggerHistoryPage | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [approving, setApproving] = useState(false);
  const [attemptsOpen, setAttemptsOpen] = useState(false);
  useEffect(() => {
    let active = true;
    setLoading(true); setError(''); setPage(null);
    const query = { appId: selectedDelivery ? undefined : scope === 'app' ? appId : undefined, targetKind: !selectedDelivery && scope === 'app' && pipelineId ? 'pipeline' as const : undefined, targetId: !selectedDelivery && scope === 'app' ? pipelineId : undefined, status, provider, ref, q: search, limit: 25, offset };
    const request = selectedDelivery ? triggerHistoryApi.delivery(selectedDelivery, query) : triggerHistoryApi.list(query);
    void request.then((result) => {
      if (!active) return;
      if (result.total > 0 && result.offset >= result.total) { setOffset(Math.floor((result.total - 1) / 25) * 25); return; }
      setPage(result);
    }).catch(() => { if (active) setError('Delivery decisions could not be loaded. Check administrator access and refresh.'); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [appId, pipelineId, scope, status, provider, ref, search, offset, revision, selectedDelivery]);
  const change = (action: () => void) => { action(); setOffset(0); };
  const closeDelivery = () => { const next = new URLSearchParams(searchParams); next.delete('gitDelivery'); setSearchParams(next); setOffset(0); };
  const deliveryLink = (id: string) => { const next = new URLSearchParams(searchParams); next.set('gitDelivery', id); return `?${next.toString()}`; };
  const resetDecisionFilters = () => { setOffset(0); setStatus(''); setProvider(''); setRef(''); setSearch(''); };
  async function approve(id: string) {
    setApproving(true); setError('');
    try { await automationApi.approve(id); setRevision((v) => v + 1); }
    catch { setError('This intent could not be approved. Refresh its source and reviewed configuration before retrying.'); }
    finally { setApproving(false); }
  }
  return <section className="section" aria-label="Git delivery history">
    <h3>Git delivery decisions</h3>
    <p>One change can produce several rule decisions. Accepted means recorded or queued; it does not confirm a successful deployment. Rules can target Cooker Apps or authored native pipelines.</p>
    {selectedDelivery && <p>Inspecting delivery <code>{selectedDelivery}</code>. <button className="hud-btn" onClick={closeDelivery}>Back to all decisions</button></p>}
    <Field label="History scope"><Select value={scope} disabled={!!selectedDelivery} onChange={(e) => change(() => setScope(e.target.value))} options={[{ value: 'app', label: pipelineId ? 'This pipeline' : 'This App' }, { value: 'all', label: 'All incoming deliveries' }]} /></Field>
    <Field label="Search deliveries" hint="Repository, ref, commit, delivery, rule, intent or job ID."><TextInput value={search} onChange={(e) => change(() => setSearch(e.target.value))} /></Field>
    <Field label="Decision status"><Select value={status} onChange={(e) => change(() => setStatus(e.target.value))} options={[{ value: '', label: 'All decisions' }, ...historyStatuses.map((value) => ({ value, label: labels[value] }))]} /></Field>
    <Field label="Git provider"><Select value={provider} onChange={(e) => change(() => setProvider(e.target.value))} options={[{ value: '', label: 'All Git providers' }, { value: 'github', label: 'GitHub' }, { value: 'gitlab', label: 'GitLab' }, { value: 'gitea', label: 'Gitea' }]} /></Field>
    <Field label="Exact source ref" hint="Optional full ref, such as refs/heads/main or refs/tags/v1.0."><TextInput value={ref} onChange={(e) => change(() => setRef(e.target.value))} /></Field>
    <Actions><button className="hud-btn" disabled={loading} onClick={() => setRevision((v) => v + 1)}>Refresh decisions</button></Actions>
    <FormError>{error}</FormError>
    {loading && <p role="status">Loading delivery decisions…</p>}
    {page && <>
      {page.journalStartedAt && <p>Retained decision audit started {new Date(page.journalStartedAt).toLocaleString()}. Earlier missing decisions remain unknown.</p>}
      <p role="status">{page.total ? `${page.offset + 1}–${Math.min(page.offset + page.items.length, page.total)} of ${page.total} decisions` : 'No matching decisions'}</p>
      {page.items.map((item) => <article key={`${item.deliveryId}:${item.decisionIndex}`} className="section" aria-label={`${labels[item.status] ?? 'Unknown'} delivery decision`}>
        <p><strong>{labels[item.status] ?? 'Unknown'}</strong> · {item.provider} · {item.repository || 'Repository not recorded'}</p>
        <p><code>{item.fullRef || 'Ref not recorded'}</code>{item.commitSha && <> · <code>{item.commitSha.slice(0, 12)}</code></>}</p>
        <p>{item.reason}</p>
        {item.intentId && item.intentStatus === 'awaiting_review' && item.status !== 'duplicate' && <button className="hud-btn" disabled={approving} onClick={() => void approve(item.intentId!)}>Approve this commit</button>}
        <p>Received {new Date(item.createdAt).toLocaleString()} · latest recorded change {new Date(item.updatedAt).toLocaleString()}{item.archived && ' · compacted history'}</p>
        {item.targetSource === 'current_rule' && <p>Target comes from the current rule; its original target was not recorded.</p>}
        {item.metadataCoverage === 'legacy' && <p>Legacy history: original decision metadata may be incomplete.</p>}
        <details><summary>Inspect delivery, intent and job</summary>
          <p>Provider delivery <code>{item.providerDeliveryId}</code></p>
          {item.ruleId && <p>Rule <code>{item.ruleId}</code> · version {item.ruleVersion ?? 'unknown'}</p>}
          {item.environmentId && <p>Original environment <code>{item.environmentId}</code></p>}
          {item.intentId && <p>Intent <code>{item.intentId}</code> · {item.intentStatus?.replace(/_/g, ' ')}</p>}
          {item.jobId && <p>Job <code>{item.jobId}</code> · {item.jobStatus?.replace(/_/g, ' ') || 'Status unavailable'}</p>}
          {item.runId && <p>Run <code>{item.runId}</code> · {item.runStatus || 'Status unavailable'}</p>}
          <Actions><Link className="hud-btn" to={deliveryLink(item.deliveryId)} onClick={resetDecisionFilters}>Inspect all delivery decisions</Link>
            {item.appId && <Link className="hud-btn" to={`/apps/${encodeURIComponent(item.appId)}`}>Open target App</Link>}
            {item.targetKind === 'pipeline' && item.targetId && <Link className="hud-btn" to={`/pipelines/${encodeURIComponent(item.targetId)}/edit`}>Open target pipeline</Link>}
            {item.pipelineId && item.runId && <Link className="hud-btn" to={item.appId ? `/apps/${encodeURIComponent(item.appId)}/deployments/${encodeURIComponent(item.pipelineId)}/${encodeURIComponent(item.runId)}` : `/pipelines/${encodeURIComponent(item.pipelineId)}/runs/${encodeURIComponent(item.runId)}`}>Open linked run</Link>}</Actions>
        </details>
      </article>)}
      <Actions><button className="hud-btn" disabled={loading || offset === 0} onClick={() => setOffset(Math.max(0, offset - 25))}>Previous decisions</button><button className="hud-btn" disabled={loading || offset + page.items.length >= page.total} onClick={() => setOffset(offset + 25)}>Next decisions</button></Actions>
      <details><summary>What this history includes</summary><ul>{page.limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul></details>
    </>}
    {(appId || sourceConnectionId) && <details onToggle={(e) => setAttemptsOpen(e.currentTarget.open)}><summary>Incoming webhook attempts</summary>{attemptsOpen && <WebhookAttemptsPanel key={appId ? `app-${appId}` : `source-${sourceConnectionId}`} appId={appId} connectionId={sourceConnectionId} onInspect={resetDecisionFilters} />}</details>}
  </section>;
}
