import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { API_ORIGIN } from '../../api/origin';
import { vercelApi } from '../../api/vercel';
import { useAuth } from '../../auth/OIDCProvider';
import type { VercelConnection, VercelLandingOperation, VercelLandingView } from '../../types/vercel';
import { timeAgo } from '../../utils/time';
import { vercelDeploymentURL } from '../../utils/vercel';
import { landingInProgress, landingReviewCurrent, landingStatus, vercelLandingPreviewURL } from '../../utils/vercelLanding';
import Badge from '../ui/Badge';
import Panel from '../ui/Panel';
import { Actions, Field, FormError, Select } from '../ui/form';
import VercelLandingAdoptions from './VercelLandingAdoptions';

// Only non-secret operation IDs are retained. An uncertain POST must not turn
// back into an enabled publish button when the user refreshes this tab.
const pendingKey = 'cooker.vercel.landing.submitted';
function storedSubmissions(): string[] {
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(pendingKey) ?? '[]');
    return Array.isArray(value) ? value.filter((id): id is string => typeof id === 'string' && id.length <= 128).slice(-100) : [];
  } catch { return []; }
}

export default function VercelLandingPublish({ connections, connectionsLoading }: { connections: VercelConnection[]; connectionsLoading: boolean }) {
  const admin = useAuth().user?.roles.includes('admin');
  const [view, setView] = useState<VercelLandingView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');
  const [connectionId, setConnectionId] = useState('');
  const [reviewId, setReviewId] = useState('');
  const [uncertainReview, setUncertainReview] = useState(false);
  const [submitted, setSubmitted] = useState<string[]>(storedSubmissions);
  const [recoveredReviews, setRecoveredReviews] = useState<string[]>([]);
  const submittedRef = useRef(submitted);
  const mounted = useRef(false);
  const generation = useRef(0);
  const actionPending = useRef(false);
  const rememberSubmissions = useCallback((ids: string[]) => {
    submittedRef.current = ids;
    setSubmitted(ids);
    try { sessionStorage.setItem(pendingKey, JSON.stringify(ids)); } catch { /* Server receipts remain authoritative without browser storage. */ }
  }, []);
  // A successful read resolves browser uncertainty. A still-reviewed request
  // can be explicitly submitted again under the same idempotent operation ID.
  const reconcileSubmissions = useCallback((current: VercelLandingOperation[]) => {
    const recovered = current.filter((operation) => operation.status === 'reviewed' && submittedRef.current.includes(operation.id)).map((operation) => operation.id);
    setRecoveredReviews((previous) => [...new Set([...previous, ...recovered])]);
    rememberSubmissions(submittedRef.current.filter((id) => !current.some((operation) => operation.id === id)));
  }, [rememberSubmissions]);
  const refresh = useCallback(async () => {
    const request = ++generation.current;
    try {
      const result = await vercelApi.landing();
      if (mounted.current && request === generation.current) {
        setView(result); setError(''); setUncertainReview(false);
        reconcileSubmissions(result.operations);
      }
    } catch (e) {
      if (mounted.current && request === generation.current) setError(e instanceof Error ? e.message : 'Landing publication is unavailable.');
    } finally { if (mounted.current && request === generation.current) setLoading(false); }
  }, [reconcileSubmissions]);
  useEffect(() => {
    mounted.current = true;
    void refresh();
    const timer = window.setInterval(() => { if (!actionPending.current) void refresh(); }, 5_000);
    return () => { mounted.current = false; window.clearInterval(timer); };
  }, [refresh]);

  const artifact = view?.artifact;
  const operations = view?.operations ?? [];
  const connection = connections.find((item) => item.id === connectionId);
  const review = operations.find((operation) => operation.id === reviewId);
  const unresolved = operations.some((operation) => landingInProgress(operation) || (operation.status === 'reviewed' && submitted.includes(operation.id)));
  const reviewCurrent = !!(review && artifact && connection && review.connectionId === connection.id && landingReviewCurrent(review, artifact.sha256, connection.version) && connection.enabled && connection.hasToken);
  const reusable = operations.find((operation) => artifact && connection && operation.connectionId === connection.id && landingReviewCurrent(operation, artifact.sha256, connection.version));
  const preview = artifact ? vercelLandingPreviewURL(artifact.previewUrl, API_ORIGIN) : undefined;

  function replace(operation: VercelLandingOperation) {
    generation.current++;
    setView((previous) => previous ? { ...previous, operations: [operation, ...previous.operations.filter((item) => item.id !== operation.id)] } : previous);
  }
  async function makeReview() {
    if (!connection || !artifact || actionPending.current || unresolved || uncertainReview || !connection.enabled || !connection.hasToken) return;
    if (reusable) { setReviewId(reusable.id); return; }
    actionPending.current = true; setBusy('review'); setError(''); setReviewId(''); generation.current++;
    try {
      const result = await vercelApi.reviewLanding(connection.id, connection.version);
      if (mounted.current) { replace(result.operation); setReviewId(result.operation.id); }
    } catch (e) {
      if (mounted.current) {
        setUncertainReview(true);
        setError(`${e instanceof Error ? e.message : 'The review response was not received.'} Refresh publication history before requesting another review.`);
      }
    } finally { actionPending.current = false; if (mounted.current) setBusy(''); }
  }
  async function deploy() {
    if (!review || !reviewCurrent || actionPending.current || unresolved || submitted.includes(review.id)) return;
    if (!artifact || !connection || !landingReviewCurrent(review, artifact.sha256, connection.version)) { setError('This review expired. Review the deployment again before publishing.'); return; }
    const ids = [...submitted, review.id].slice(-100);
    rememberSubmissions(ids);
    actionPending.current = true; setBusy(review.id); setError(''); generation.current++;
    try {
      const result = await vercelApi.deployLanding(review.id);
      if (mounted.current) { replace(result.operation); setReviewId(''); }
    } catch (e) {
      if (mounted.current) {
        setReviewId('');
        setError(`${e instanceof Error ? e.message : 'The deployment response was not received.'} Submission is not confirmed. Check this operation before starting another publication.`);
      }
    } finally { actionPending.current = false; if (mounted.current) setBusy(''); }
  }
  async function check(operation: VercelLandingOperation) {
    if (actionPending.current || operation.status !== 'needs_attention') return;
    actionPending.current = true; setBusy(operation.id); setError(''); generation.current++;
    try { const result = await vercelApi.checkLanding(operation.id); if (mounted.current) replace(result.operation); }
    catch (e) { if (mounted.current) setError(e instanceof Error ? e.message : 'Could not check Vercel. No new deployment was submitted.'); }
    finally { actionPending.current = false; if (mounted.current) setBusy(''); }
  }
  async function refreshOperation(operation: VercelLandingOperation) {
    if (actionPending.current) return;
    actionPending.current = true; setBusy(operation.id); setError(''); generation.current++;
    try {
      const result = await vercelApi.landingOperation(operation.id);
      if (mounted.current) { replace(result.operation); reconcileSubmissions([result.operation]); }
    } catch (e) { if (mounted.current) setError(e instanceof Error ? e.message : 'Could not read this request. No deployment was submitted.'); }
    finally { actionPending.current = false; if (mounted.current) setBusy(''); }
  }

  return <><Panel title="Publish Cooker landing">
    <p>Review the prepared landing page, then publish it to a new dedicated Vercel project.</p>
    <FormError>{error}</FormError>
    {loading && <p role="status">Loading landing artifact and publication history…</p>}
    {error && <button type="button" className="hud-btn" disabled={!!busy} onClick={() => void refresh()}>Refresh publication history</button>}
    {artifact && <>
      <div className="vercel-card vercel-stack">
        <div className="vercel-heading"><h3>{artifact.name}</h3><Badge variant="muted">Prepared static site</Badge></div>
        <div className="vercel-meta">{artifact.fileCount.toLocaleString()} files · {artifact.bytes.toLocaleString()} bytes</div>
        <details><summary>Exact artifact</summary><p className="vercel-meta">SHA-256 <code>{artifact.sha256}</code></p></details>
        {preview && <a className="vercel-inline-link" href={preview} target="_blank" rel="noopener noreferrer">Preview landing ↗</a>}
        <p className="vercel-hint">This publishes only the landing page and its visual assets. The Cooker application and backend are not included.</p>
      </div>
      {admin ? <div className="vercel-stack">
        <Field label="Publish to Vercel account or team" hint="Cooker creates a new dedicated project in the account you choose."><Select value={connectionId} disabled={!!busy || connectionsLoading} onChange={(event) => { setConnectionId(event.target.value); setReviewId(''); }} options={[{ value: '', label: 'Choose an account or team' }, ...connections.filter((item) => item.enabled && item.hasToken).map((item) => ({ value: item.id, label: `${item.name} · ${item.identity.name}` }))]} /></Field>
        {!connectionsLoading && !connections.some((item) => item.enabled && item.hasToken) && <p><Link className="vercel-inline-link" to="/settings#vercel-connections">Connect Vercel in settings</Link> to prepare a publication.</p>}
        {unresolved && <p className="vercel-hint" role="status">A publication is still in progress or needs checking. Use its existing receipt below before starting another.</p>}
        <Actions><button type="button" className="hud-btn" disabled={!!busy || !connection || !connection.enabled || !connection.hasToken || unresolved || uncertainReview} onClick={() => void makeReview()}>{busy === 'review' ? 'Preparing review…' : 'Review deployment'}</button></Actions>
        {review && review.connectionId === connectionId && <div className="vercel-card vercel-stack" aria-label="Landing deployment review">
          <div className="vercel-heading"><h3>Review before publishing</h3><Badge variant="ember">Production · first release</Badge></div>
          <dl className="vercel-review-details">
            <dt>Account / team</dt><dd>{review.accountName}</dd>
            <dt>New project</dt><dd><code>{review.projectName}</code></dd>
            <dt>Files</dt><dd>{artifact.fileCount.toLocaleString()} files · {artifact.bytes.toLocaleString()} bytes</dd>
            <dt>Artifact SHA-256</dt><dd><code>{review.artifactSha256}</code></dd>
            <dt>Review expires</dt><dd><time dateTime={review.expiresAt}>{new Date(review.expiresAt).toLocaleString()}</time></dd>
          </dl>
          <p>This creates a new dedicated static project and publishes its first production release. No custom domain will be added.</p>
          {recoveredReviews.includes(review.id) && <p className="vercel-hint">Cooker still shows this request as reviewed. You can submit this same request again; its existing operation ID prevents a second job from being created.</p>}
          {!reviewCurrent && <p className="vercel-hint" role="status">This review expired or its artifact or connection changed. Review the current configuration again.</p>}
          <Actions><button type="button" className="hud-btn hud-btn-primary vercel-publish-action" disabled={!!busy || !reviewCurrent || unresolved || submitted.includes(review.id)} onClick={() => void deploy()}>{recoveredReviews.includes(review.id) ? 'Submit reviewed request' : 'Create project & publish landing'}</button></Actions>
        </div>}
      </div> : <p className="vercel-hint">An administrator can review and publish this landing page. Publication receipts are shown below.</p>}
    </>}
    <ul className="vercel-history" aria-label="Cooker landing publication history">
      {operations.map((operation) => {
        const wasSubmitted = submitted.includes(operation.id);
        const status = operation.status === 'reviewed' && wasSubmitted ? { label: 'Submission not confirmed', variant: 'ember' as const } : operation.status === 'reviewed' && Date.parse(operation.expiresAt) <= Date.now() ? { label: 'Review expired', variant: 'muted' as const } : landingStatus(operation.status);
        const url = vercelDeploymentURL(operation.url ?? '');
        const canCheck = operation.status === 'needs_attention';
        const canRefresh = (!canCheck && landingInProgress(operation)) || (operation.status === 'reviewed' && wasSubmitted);
        return <li key={operation.id} className="vercel-card" aria-label={`Publication ${operation.projectName}`}>
          <div className="vercel-heading"><h3>{operation.projectName}</h3><Badge variant={status.variant}>{status.label}</Badge></div>
          <p className="vercel-meta">{operation.accountName} · Production · {timeAgo(operation.updatedAt)}</p>
          <p className="vercel-meta">Cooker operation <code>{operation.id}</code></p>
          {operation.projectId && <p className="vercel-meta">Vercel project <code>{operation.projectId}</code></p>}
          {operation.deploymentId && <p className="vercel-meta">Vercel deployment <code>{operation.deploymentId}</code></p>}
          {operation.message && <p className="vercel-hint">{operation.message}</p>}
          {canCheck && <p className="vercel-hint">Vercel may already have accepted this request. Checking the provider does not submit another deployment.</p>}
          {canRefresh && <p className="vercel-hint">Refresh reads this existing request. It does not submit a deployment.</p>}
          {url && <a href={url} target="_blank" rel="noopener noreferrer">{operation.status === 'succeeded' ? 'Open published landing ↗' : 'Open Vercel deployment ↗'}</a>}
          <Actions>
            {canRefresh && <button type="button" className="hud-btn" disabled={!!busy} onClick={() => void refreshOperation(operation)}>{busy === operation.id ? 'Refreshing…' : 'Refresh status'}</button>}
            {admin && canCheck && <button type="button" className="hud-btn" disabled={!!busy} onClick={() => void check(operation)}>{busy === operation.id ? 'Checking…' : 'Check provider'}</button>}
            {admin && operation.status === 'reviewed' && !wasSubmitted && !unresolved && <button type="button" className="hud-btn" disabled={!!busy} onClick={() => { setConnectionId(operation.connectionId); setReviewId(operation.id); }}>Open review</button>}
          </Actions>
        </li>;
      })}
    </ul>
    {!loading && view && !operations.length && <p className="vercel-hint">No landing publication has been requested yet.</p>}
  </Panel><VercelLandingAdoptions connections={connections} connectionsLoading={connectionsLoading} operations={operations} operationsLoading={loading} operationsAvailable={!!view && !error} currentArtifact={artifact} artifactAvailable={!!view && !error} /></>;
}
