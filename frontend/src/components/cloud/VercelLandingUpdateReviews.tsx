import { useCallback, useEffect, useRef, useState } from 'react';
import { vercelLandingUpdateReviewsApi } from '../../api/vercelLandingUpdateReviews';
import { useAuth } from '../../auth/OIDCProvider';
import type { VercelConnection, VercelLandingArtifact } from '../../types/vercel';
import type { VercelLandingAdoption } from '../../types/vercelLandingAdoption';
import type { VercelLandingUpdateReview } from '../../types/vercelLandingUpdateReview';
import { landingReviewDestination, landingUpdateReviewCandidates, landingUpdateReviewState, type LandingUpdateReviewState } from '../../utils/vercelLandingUpdateReviews';
import Badge from '../ui/Badge';
import Panel from '../ui/Panel';
import { Actions, Field, FormError, Select } from '../ui/form';

interface Props {
  adoptions: VercelLandingAdoption[];
  adoptionsAvailable: boolean;
  adoptionsLoading: boolean;
  connections: VercelConnection[];
  connectionsLoading: boolean;
  currentArtifact?: VercelLandingArtifact;
  artifactAvailable?: boolean;
}

function dateLabel(value: string): string {
  return Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString() : 'Time unavailable';
}

function numberLabel(value: number): string {
  return Number.isFinite(value) && value >= 0 ? value.toLocaleString() : 'Unavailable';
}

export function LandingUpdateReviewRecord({ review, state }: { review: VercelLandingUpdateReview; state: LandingUpdateReviewState }) {
  const addresses = Array.isArray(review.baselineAliases) ? review.baselineAliases : [];
  const safeAddresses = [...new Set(addresses.map(landingReviewDestination).filter((value): value is string => value !== undefined))];
  return <li className="vercel-card vercel-stack" aria-label={`Update review for ${review.projectName}`}>
    <div className="vercel-heading"><h3>{review.projectName}</h3><Badge variant={state.variant}>{state.label}</Badge></div>
    <p className="vercel-hint">{state.explanation}</p>
    <p className="vercel-hint">Inspection only. This review cannot submit a deployment or change the Vercel project.</p>
    <dl className="vercel-review-details">
      <dt>Existing project ID</dt><dd><code>{review.projectId}</code></dd>
      <dt>Account / team ID</dt><dd><code>{review.accountId}</code></dd>
      <dt>Artifact for this review</dt><dd><code>{review.artifactSha256}</code></dd>
      <dt>Artifact contents</dt><dd>{numberLabel(review.artifactFileCount)} files · {numberLabel(review.artifactBytes)} bytes</dd>
      <dt>Production observed at review</dt><dd><code>{review.baselineDeploymentId}</code></dd>
      <dt>Production observation</dt><dd><time dateTime={review.baselineDeploymentObservedAt}>{dateLabel(review.baselineDeploymentObservedAt)}</time></dd>
      <dt>Review created</dt><dd><time dateTime={review.createdAt}>{dateLabel(review.createdAt)}</time></dd>
      <dt>Review expires</dt><dd><time dateTime={review.expiresAt}>{dateLabel(review.expiresAt)}</time></dd>
    </dl>
    <div>
      <h4>Addresses observed at review</h4>
      <p className="vercel-meta">Observed <time dateTime={review.baselineAliasesObservedAt}>{dateLabel(review.baselineAliasesObservedAt)}</time>. Routing may have changed; public access and application health were not checked.</p>
      {safeAddresses.length > 0 && <ul>{safeAddresses.map((address) => <li key={address}><a className="vercel-inline-link" href={address} target="_blank" rel="noopener noreferrer">{address} ↗</a></li>)}</ul>}
      {Array.isArray(review.baselineAliases) && addresses.length === 0 && <p className="vercel-hint">No aliases were reported for the baseline deployment at review time.</p>}
      {(!Array.isArray(review.baselineAliases) || addresses.some((value) => landingReviewDestination(value) === undefined)) && <p className="vercel-hint">Some recorded address details are unavailable.</p>}
    </div>
    <details><summary>Exact scope and provenance</summary><dl className="vercel-review-details">
      <dt>Review ID</dt><dd><code>{review.id}</code></dd>
      <dt>Adoption ID / version</dt><dd><code>{review.adoptionId}</code> · {review.adoptionVersion}</dd>
      <dt>Connection ID / version</dt><dd><code>{review.connectionId}</code> · {review.connectionVersion}</dd>
      <dt>Environment</dt><dd>{review.environment}</dd>
      <dt>Deployment owner</dt><dd>{review.deploymentOwner}</dd>
      <dt>Source kind</dt><dd>{review.sourceKind}</dd>
      <dt>Reviewed action</dt><dd>{review.action}</dd>
      <dt>Original publication</dt><dd><code>{review.provenancePublicationId}</code></dd>
      <dt>Original deployment</dt><dd><code>{review.provenanceDeploymentId}</code></dd>
      <dt>Original artifact</dt><dd><code>{review.provenanceArtifactSha256}</code></dd>
      <dt>Project support observed</dt><dd>{review.assessment?.observedAt ? <time dateTime={review.assessment.observedAt}>{dateLabel(review.assessment.observedAt)}</time> : 'Not observed'}</dd>
    </dl></details>
  </li>;
}

export default function VercelLandingUpdateReviews({ adoptions, adoptionsAvailable, adoptionsLoading, connections, connectionsLoading, currentArtifact, artifactAvailable = false }: Props) {
  const user = useAuth().user;
  const subject = user?.sub ?? '';
  const admin = user?.roles.includes('admin') === true;
  const [reviews, setReviews] = useState<VercelLandingUpdateReview[] | null>(null);
  const [loadedFor, setLoadedFor] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [selection, setSelection] = useState('');
  const [now, setNow] = useState(Date.now);
  const mounted = useRef(false);
  const session = useRef(0);
  const generation = useRef(0);
  const actionPending = useRef(false);
  const readController = useRef<AbortController | null>(null);
  const actionController = useRef<AbortController | null>(null);

  const refresh = useCallback(async () => {
    if (!subject || actionPending.current) return;
    const epoch = session.current;
    const request = ++generation.current;
    readController.current?.abort();
    const controller = new AbortController();
    readController.current = controller;
    setLoading(true);
    setSelection('');
    try {
      const result = await vercelLandingUpdateReviewsApi.list(controller.signal);
      if (result.reviews !== null && !Array.isArray(result.reviews)) throw new Error('Invalid update review history');
      if (mounted.current && epoch === session.current && request === generation.current) {
        setReviews(result.reviews ?? []);
        setLoadedFor(subject);
        setNow(Date.now());
        setError('');
        setNotice('');
        setUncertain(false);
      }
    } catch {
      if (mounted.current && epoch === session.current && request === generation.current) {
        setError('Update review history could not be refreshed. Any receipts below are from the last successful read.');
      }
    } finally {
      if (mounted.current && epoch === session.current && request === generation.current) setLoading(false);
    }
  }, [subject]);

  useEffect(() => {
    mounted.current = true;
    session.current++;
    actionPending.current = false;
    setReviews(null);
    setLoadedFor('');
    setSelection('');
    setError('');
    setNotice('');
    setUncertain(false);
    setBusy(false);
    if (subject) void refresh();
    else setLoading(false);
    return () => {
      mounted.current = false;
      session.current++;
      generation.current++;
      readController.current?.abort();
      actionController.current?.abort();
    };
  }, [subject, admin, refresh]);

  const ready = loadedFor === subject && reviews !== null;
  const rows = ready ? reviews : [];
  const candidates = adoptionsAvailable && !adoptionsLoading && !connectionsLoading
    ? landingUpdateReviewCandidates(adoptions, connections) : [];
  const selected = candidates.find((item) => item.key === selection);
  const canPrepare = admin && ready && !loading && !error && !uncertain && !busy
    && adoptionsAvailable && !adoptionsLoading && !connectionsLoading && !!selected;
  const nextExpiry = rows.reduce((next, review) => {
    const expires = Date.parse(review.expiresAt);
    return Number.isFinite(expires) && expires > now ? Math.min(next, expires) : next;
  }, Infinity);

  // A local clock update marks receipts expired without polling the API.
  useEffect(() => {
    if (!Number.isFinite(nextExpiry)) return;
    const timer = window.setTimeout(() => setNow(Date.now()), Math.min(Math.max(1, nextExpiry - now + 1), 2_147_483_647));
    return () => window.clearTimeout(timer);
  }, [nextExpiry, now]);

  async function prepare() {
    if (!canPrepare || !selected || actionPending.current) return;
    const epoch = session.current;
    const controller = new AbortController();
    actionController.current = controller;
    actionPending.current = true;
    generation.current++;
    readController.current?.abort();
    setBusy(true);
    setError('');
    setNotice('');
    setSelection('');
    try {
      const result = await vercelLandingUpdateReviewsApi.create({
        adoptionId: selected.adoption.id,
        adoptionVersion: selected.adoption.version,
        connectionVersion: selected.connection.version,
      }, controller.signal);
      if (mounted.current && epoch === session.current) {
        setReviews((previous) => [result.review, ...(previous ?? []).filter((item) => item.id !== result.review.id)].slice(0, 100));
        setNow(Date.now());
        setNotice('Update review recorded. No deployment or job was submitted.');
      }
    } catch {
      if (mounted.current && epoch === session.current) {
        setUncertain(true);
        setError('The review response was not confirmed. Refresh update review history before preparing another review. Cooker will not resubmit the request automatically.');
      }
    } finally {
      if (epoch === session.current) {
        actionPending.current = false;
        if (mounted.current) setBusy(false);
      }
    }
  }

  return <Panel title="Landing update reviews">
    <p>Inspect the current embedded landing bundle and the production destination of an adopted project before a future update.</p>
    <p className="vercel-hint">This feature prepares inspection receipts only. Deployment submission is unavailable.</p>
    <Actions><button type="button" className="hud-btn" disabled={!subject || loading || busy} onClick={() => void refresh()}>Refresh update reviews</button></Actions>
    <FormError>{error}</FormError>
    {notice && <p className="vercel-hint" role="status">{notice}</p>}
    {loading && <p role="status">Loading existing-project update reviews…</p>}
    {uncertain && <p className="vercel-hint">A receipt may already have been saved. Refresh reads existing reviews and does not retry the request.</p>}
    {admin && ready && <div className="vercel-stack">
      <Field label="Adopted production landing" hint="Only an enabled adoption matching the currently selected connection and accepted versions can be reviewed.">
        <Select value={selected ? selection : ''} disabled={loading || busy || !!error || uncertain || adoptionsLoading || connectionsLoading || !adoptionsAvailable} onChange={(event) => setSelection(event.target.value)} options={[
          { value: '', label: 'Choose an adopted landing project' },
          ...candidates.map((item) => ({ value: item.key, label: `${item.adoption.projectName} · ${item.adoption.projectId} · adoption v${item.adoption.version}` })),
        ]} />
      </Field>
      {selected && <p className="vercel-meta">Existing project <code>{selected.adoption.projectId}</code> · account <code>{selected.adoption.accountId}</code> · adoption v{selected.adoption.version} · connection v{selected.connection.version}</p>}
      {!adoptionsLoading && !connectionsLoading && adoptionsAvailable && candidates.length === 0 && <p className="vercel-hint">No current adoption is eligible for review. Check its enabled state, project selection and accepted connection version above.</p>}
      {!adoptionsLoading && !adoptionsAvailable && <p className="vercel-hint">Current adoption records are unavailable. Refresh adoption history above first.</p>}
      <Actions><button type="button" className="hud-btn" disabled={!canPrepare} onClick={() => void prepare()}>{busy ? 'Preparing review…' : 'Prepare update review'}</button></Actions>
    </div>}
    {!admin && <p className="vercel-hint">Review history is read-only for your role. An administrator can prepare a new review.</p>}
    <ul className="vercel-history" aria-label="Existing-project update review history">
      {rows.map((review) => <LandingUpdateReviewRecord key={review.id} review={review} state={landingUpdateReviewState(review, {
        adoptions, connections, adoptionsAvailable: adoptionsAvailable && !adoptionsLoading,
        connectionsLoading, currentArtifact, artifactAvailable, now,
      })} />)}
    </ul>
    {ready && !loading && !error && rows.length === 0 && <p className="vercel-hint">No existing-project update review has been prepared.</p>}
    <p className="vercel-hint">Up to 100 recent review receipts are shown. They describe their recorded observations, not current routing or application health.</p>
  </Panel>;
}
