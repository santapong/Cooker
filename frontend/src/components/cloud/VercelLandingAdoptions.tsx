import { useCallback, useEffect, useRef, useState } from 'react';
import { vercelLandingAdoptionsApi } from '../../api/vercelLandingAdoptions';
import { useAuth } from '../../auth/OIDCProvider';
import type { VercelConnection, VercelLandingArtifact } from '../../types/vercel';
import type { VercelLandingAdoption, VercelLandingProvenance } from '../../types/vercelLandingAdoption';
import { landingAdoptionCandidates, landingAdoptionConnection, landingAdoptionScopeSupported, landingAdoptionState, landingCandidateConfirmation, landingReauthorizationConfirmation } from '../../utils/vercelLandingAdoptions';
import Badge from '../ui/Badge';
import Panel from '../ui/Panel';
import { Actions, Check, Field, FormError, Select } from '../ui/form';
import VercelLandingUpdateReviews from './VercelLandingUpdateReviews';

interface Props {
  connections: VercelConnection[];
  connectionsLoading: boolean;
  operations: VercelLandingProvenance[];
  operationsLoading: boolean;
  operationsAvailable: boolean;
  currentArtifact?: VercelLandingArtifact;
  artifactAvailable?: boolean;
}

function dateLabel(value: string): string {
  return Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString() : 'Time unavailable';
}

const confirmation = 'I authorize manual Cooker updates of this existing production project using only the embedded Cooker landing bundle.';

export default function VercelLandingAdoptions({ connections, connectionsLoading, operations, operationsLoading, operationsAvailable, currentArtifact, artifactAvailable = false }: Props) {
  const user = useAuth().user;
  const subject = user?.sub ?? '';
  const admin = user?.roles.includes('admin') === true;
  const [adoptions, setAdoptions] = useState<VercelLandingAdoption[] | null>(null);
  const [loadedFor, setLoadedFor] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [uncertain, setUncertain] = useState(false);
  const [busy, setBusy] = useState('');
  const [candidateKey, setCandidateKey] = useState('');
  const [confirmed, setConfirmed] = useState('');
  const [confirmedAdoption, setConfirmedAdoption] = useState('');
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
    setConfirmed('');
    setConfirmedAdoption('');
    try {
      const result = await vercelLandingAdoptionsApi.list(controller.signal);
      if (result.adoptions !== null && !Array.isArray(result.adoptions)) throw new Error('Invalid adoption history');
      if (mounted.current && epoch === session.current && request === generation.current) {
        setAdoptions(result.adoptions ?? []);
        setLoadedFor(subject);
        setError('');
        setUncertain(false);
        setNotice('');
      }
    } catch {
      if (mounted.current && epoch === session.current && request === generation.current) {
        setError('Adoption history could not be refreshed. Any records below are from the last successful read.');
      }
    } finally {
      if (mounted.current && epoch === session.current && request === generation.current) setLoading(false);
    }
  }, [subject]);

  useEffect(() => {
    mounted.current = true;
    session.current++;
    actionPending.current = false;
    setAdoptions(null);
    setLoadedFor('');
    setCandidateKey('');
    setConfirmed('');
    setConfirmedAdoption('');
    setBusy('');
    setError('');
    setNotice('');
    setUncertain(false);
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

  const ready = loadedFor === subject && adoptions !== null;
  const rows = ready ? adoptions : [];
  const candidates = ready && !connectionsLoading && operationsAvailable
    ? landingAdoptionCandidates(operations, connections, rows) : [];
  const selected = candidates.find((item) => item.key === candidateKey);
  const editsAvailable = admin && ready && !loading && !busy && !error && !uncertain;

  async function save(action: { kind: 'create' } | { kind: 'update'; adoption: VercelLandingAdoption; enabled: boolean }) {
    if (!editsAvailable || actionPending.current) return;
    const current = action.kind === 'update' ? rows.find((item) => item.id === action.adoption.id && item.version === action.adoption.version) : undefined;
    const connection = current ? landingAdoptionConnection(current, connections) : selected?.connection;
    if (action.kind === 'create' && (!selected || confirmed !== landingCandidateConfirmation(selected) || connectionsLoading || !operationsAvailable)) return;
    if (action.kind === 'update' && (!current || !Number.isInteger(current.version) || current.version < 1
      || (action.enabled && (!connection || connectionsLoading || confirmedAdoption !== landingReauthorizationConfirmation(current, connection))))) return;
    const epoch = session.current;
    const controller = new AbortController();
    actionController.current = controller;
    actionPending.current = true;
    generation.current++;
    readController.current?.abort();
    setBusy(current?.id ?? 'create');
    setError('');
    setNotice('');
    setConfirmed('');
    setConfirmedAdoption('');
    try {
      const result = action.kind === 'create'
        ? await vercelLandingAdoptionsApi.create({
          connectionId: selected!.connection.id, connectionVersion: selected!.connection.version,
          projectId: selected!.publication.projectId!, provenancePublicationId: selected!.publication.id,
          confirmManualUpdates: true,
        }, controller.signal)
        : await vercelLandingAdoptionsApi.update(current!.id, {
          version: current!.version,
          connectionVersion: action.enabled ? connection!.version : current!.connectionVersion,
          enabled: action.enabled, confirmManualUpdates: action.enabled,
        }, controller.signal);
      if (mounted.current && epoch === session.current) {
        setAdoptions((previous) => [result.adoption, ...(previous ?? []).filter((item) => item.id !== result.adoption.id)]);
        setCandidateKey('');
        setNotice(result.adoption.enabled
          ? 'Adoption recorded. No deployment was submitted and no Vercel settings were changed.'
          : 'Adoption revoked locally. The Vercel project and historical receipts are retained.');
      }
    } catch {
      if (mounted.current && epoch === session.current) {
        setUncertain(true);
        setError('The save response was not confirmed. Refresh adoption history before making another change. Cooker will not resubmit this request automatically.');
      }
    } finally {
      if (epoch === session.current) {
        actionPending.current = false;
        if (mounted.current) setBusy('');
      }
    }
  }

  return <><Panel title="Existing landing project" aside={<button type="button" className="hud-btn" disabled={loading || !!busy || !subject} onClick={() => void refresh()}>Refresh adoption history</button>}>
    <p>Keep the existing landing project and address. An administrator can record a limited scope for future manual updates.</p>
    <p className="vercel-hint">This records local authority and checks project support. It does not create a project, submit a deployment, or change Vercel settings.</p>
    <FormError>{error}</FormError>
    {notice && <p className="vercel-hint" role="status">{notice}</p>}
    {loading && <p role="status">Loading existing-project adoption history…</p>}
    {uncertain && <p className="vercel-hint">A save may already have completed. The next step reads existing records; it does not retry the save.</p>}
    {admin && ready && <div className="vercel-stack">
      <Field label="Existing published landing" hint="Candidates require an exact successful production publication receipt and a current connection selecting that same project.">
        <Select value={selected ? candidateKey : ''} disabled={!editsAvailable || connectionsLoading || operationsLoading} onChange={(event) => { setCandidateKey(event.target.value); setConfirmed(''); }} options={[
          { value: '', label: 'Choose a published landing project' },
          ...candidates.map((item) => ({ value: item.key, label: `${item.publication.projectName} · ${item.connection.identity.name} · ${item.publication.projectId}` })),
        ]} />
      </Field>
      {!connectionsLoading && !operationsLoading && operationsAvailable && candidates.length === 0 && <p className="vercel-hint">No additional matching landing is available to adopt. Existing adoptions remain below. Missing account identity, an unselected project, or a disconnected connection cannot be matched by name.</p>}
      {!operationsLoading && !operationsAvailable && <p className="vercel-hint">Publication receipts are unavailable. Refresh publication history above before choosing an existing landing.</p>}
      {selected && <div className="vercel-card vercel-stack">
        <h3>Review the exact project</h3>
        <dl className="vercel-review-details">
          <dt>Account / team ID</dt><dd><code>{selected.connection.identity.id}</code></dd>
          <dt>Existing project ID</dt><dd><code>{selected.publication.projectId}</code></dd>
          <dt>Successful publication</dt><dd><code>{selected.publication.id}</code></dd>
          <dt>Recorded deployment</dt><dd><code>{selected.publication.deploymentId}</code></dd>
          <dt>Connection version</dt><dd>{selected.connection.version}</dd>
          <dt>Fixed scope</dt><dd>Production · manual Cooker updates · embedded Cooker landing bundle · update landing only</dd>
        </dl>
        <Check label={confirmation} checked={confirmed === landingCandidateConfirmation(selected)} disabled={!editsAvailable} onChange={(value) => setConfirmed(value ? landingCandidateConfirmation(selected) : '')} />
        <Actions><button type="button" className="hud-btn hud-btn-primary" disabled={!editsAvailable || confirmed !== landingCandidateConfirmation(selected) || connectionsLoading} onClick={() => void save({ kind: 'create' })}>{busy === 'create' ? 'Recording adoption…' : 'Record adoption'}</button></Actions>
      </div>}
    </div>}
    {!admin && <p className="vercel-hint">Adoption records are read-only for your role. An administrator manages the permitted scope.</p>}
    <ul className="vercel-history" aria-label="Existing landing adoption records">
      {rows.map((adoption) => {
        const state = landingAdoptionState(adoption, connections, connectionsLoading);
        const connection = landingAdoptionConnection(adoption, connections);
        return <li key={adoption.id} className="vercel-card vercel-stack">
          <div className="vercel-heading"><h3>{adoption.projectName}</h3><Badge variant={state.variant}>{state.label}</Badge></div>
          <p className="vercel-hint">{state.explanation}</p>
          <p className="vercel-meta">{landingAdoptionScopeSupported(adoption) ? 'Production · manual Cooker updates · embedded Cooker landing bundle' : 'Scope unavailable — administrator review required'}</p>
          <p className="vercel-meta">Project <code>{adoption.projectId}</code> · Adoption version {adoption.version}</p>
          <p className="vercel-meta">Last saved: <time dateTime={adoption.updatedAt}>{dateLabel(adoption.updatedAt)}</time></p>
          <details><summary>Identity and assessment receipt</summary><dl className="vercel-review-details">
            <dt>Adoption ID</dt><dd><code>{adoption.id}</code></dd>
            <dt>Account / team ID</dt><dd><code>{adoption.accountId}</code></dd>
            <dt>Connection ID</dt><dd><code>{adoption.connectionId}</code></dd>
            <dt>Accepted connection version</dt><dd>{adoption.connectionVersion}</dd>
            <dt>Current connection version</dt><dd>{connection?.version ?? 'Unavailable'}</dd>
            <dt>Original publication</dt><dd><code>{adoption.provenancePublicationId}</code></dd>
            <dt>Original deployment</dt><dd><code>{adoption.provenanceDeploymentId}</code></dd>
            <dt>Project support checked</dt><dd>{adoption.assessment?.observedAt ? <time dateTime={adoption.assessment.observedAt}>{dateLabel(adoption.assessment.observedAt)}</time> : 'Not observed'}</dd>
            <dt>Production at that check</dt><dd>{adoption.assessment?.productionDeploymentId ? <code>{adoption.assessment.productionDeploymentId}</code> : 'Not observed'}</dd>
          </dl></details>
          {admin && <>
            {connection && <Check label={confirmation} checked={confirmedAdoption === landingReauthorizationConfirmation(adoption, connection)} disabled={!editsAvailable || connectionsLoading} onChange={(value) => setConfirmedAdoption(value ? landingReauthorizationConfirmation(adoption, connection) : '')} />}
            <Actions>
              {connection && <button type="button" className="hud-btn" disabled={!editsAvailable || connectionsLoading || confirmedAdoption !== landingReauthorizationConfirmation(adoption, connection)} onClick={() => void save({ kind: 'update', adoption, enabled: true })}>{busy === adoption.id ? 'Saving…' : 'Recheck & reauthorize'}</button>}
              {adoption.enabled && <button type="button" className="hud-btn" disabled={!editsAvailable} onClick={() => void save({ kind: 'update', adoption, enabled: false })}>{busy === adoption.id ? 'Saving…' : 'Revoke adoption'}</button>}
            </Actions>
          </>}
        </li>;
      })}
    </ul>
    {ready && !loading && !error && rows.length === 0 && <p className="vercel-hint">No existing landing project has been adopted.</p>}
  </Panel><VercelLandingUpdateReviews adoptions={rows} adoptionsAvailable={ready && !error && !uncertain} adoptionsLoading={loading || !!busy} connections={connections} connectionsLoading={connectionsLoading} currentArtifact={currentArtifact} artifactAvailable={artifactAvailable} /></>;
}
