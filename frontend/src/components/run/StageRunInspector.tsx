import { useEffect, useState } from 'react';
import type { Stage, StageApproval, StageRun } from '../../types/pipeline';
import { pipelineApi, runtimeApi, type ServiceRuntimeStatus } from '../../api/pipelines';
import { useCapabilitiesStore } from '../../stores/capabilitiesStore';
import Badge from '../ui/Badge';
import Caps from '../ui/Caps';
import StageSymbol from '../pipeline/StageSymbol';
import { formatDuration, stageDurationMs, statusVariant } from '../porthole/runState';
import type { GateLoadState } from './approvalPolicy';
import { deploymentRuntimeService } from './deploymentRuntime';
import NativeRuntimePanel from './NativeRuntimePanel';
import type { NativeRuntimeStatus } from '../../types/nativeRuntime';

function clock(iso: string | null | undefined): string | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d.toLocaleTimeString();
}

interface Props {
  stage: Stage;
  stageRun?: StageRun;
  gate?: StageApproval;
  now: number;
  onClose: () => void;
  onGate?: (kind: 'approve' | 'reject', note: string) => Promise<void>;
  gateState?: GateLoadState;
  acceptedVersion?: number;
  currentVersion?: number;
  retainedGate?: boolean;
  canTriage?: boolean;
  /** Deployment view: the app whose recorded service is inspected. */
  appId?: string;
  /** Run coordinates for AI triage of a failed stage. */
  pipelineId?: string;
  runId?: string;
  onNativeRuntimeStatus?: (status: NativeRuntimeStatus | null) => void;
}

/** Right-hand inspector for a stage of a run: timing, error, artifacts, outputs, approval gate, runtime. */
export default function StageRunInspector({ stage, stageRun, gate, now, onClose, onGate, gateState = 'unavailable', acceptedVersion, currentVersion, retainedGate = false, canTriage = false, appId, pipelineId, runId, onNativeRuntimeStatus }: Props) {
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState<'approve' | 'reject' | null>(null);
  const [runtime, setRuntime] = useState<ServiceRuntimeStatus | null>(null);
  const [triage, setTriage] = useState<{ advisory: string; model: string } | null>(null);
  const [triaging, setTriaging] = useState(false);
  const [triageError, setTriageError] = useState<string | null>(null);
  const [gateError, setGateError] = useState<string | null>(null);
  const aiTriage = useCapabilitiesStore((s) => s.capabilities?.aiTriage ?? false);
  const service = appId ? deploymentRuntimeService(stage)?.id : undefined;
  useEffect(() => {
    setTriage(null);
    setTriageError(null);
    setNote('');
    setGateError(null);
  }, [stage.id, runId]);
  const runTriage = async () => {
    if (!pipelineId || !runId) return;
    setTriaging(true);
    setTriageError(null);
    try {
      setTriage(await pipelineApi.triageStage(pipelineId, runId, stage.id));
    } catch {
      setTriageError('This failure could not be explained. Check access and retry.');
    } finally {
      setTriaging(false);
    }
  };

  useEffect(() => {
    setRuntime(null);
    if (!appId || !service) return;
    let cancelled = false;
    runtimeApi
      .serviceStatus(appId, service)
      .then((s) => {
        if (!cancelled) setRuntime(s);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [appId, service, stageRun?.status]);

  const duration = stageDurationMs(stageRun, now);
  const awaiting = gate?.status === 'awaiting';
  const canDecide = !!onGate && gateState === 'ready' && awaiting;

  const act = async (kind: 'approve' | 'reject') => {
    if (!onGate || !canDecide) return;
    setBusy(kind);
    setGateError(null);
    try {
      await onGate(kind, note);
      setNote('');
    } catch {
      setGateError('This decision could not be confirmed. Your note is retained. Refresh the gate before trying again.');
    } finally {
      setBusy(null);
    }
  };

  return (
    <aside className="inspector" aria-label={`Stage ${stage.name}`}>
      <div className="inspector-head">
        <div className="inspector-status">
          <StageSymbol kind={stage.type} />
          <Badge variant="muted">{stage.type}</Badge>
          <Badge variant={statusVariant(stageRun?.status)}>{stageRun?.status ?? 'not run'}</Badge>
        </div>
        <span className="spacer" />
        <button type="button" className="inspector-close" onClick={onClose} aria-label="Close inspector">
          ×
        </button>
      </div>
      <h2>{stage.name}</h2>
      {(gate || retainedGate) && <p>Recorded approval gate · stage ID {stage.id} · accepted pipeline version {acceptedVersion ?? 'unavailable'}{currentVersion !== acceptedVersion && ` · current definition version ${currentVersion ?? 'unavailable'}`}. {retainedGate ? 'This gate is absent from the current definition; its original stage name and configuration are not reconstructed.' : currentVersion !== acceptedVersion ? 'The displayed stage name and type come from the current definition.' : ''}</p>}
      {stage.type === 'approval' && !gate && <p role="status">No verified gate record is available for this stage. Refresh approval gates before recording a decision.</p>}

      <div className="kv">
        <Caps>Started</Caps>
        <span className={clock(stageRun?.startedAt) ? 'v' : 'v muted'}>{clock(stageRun?.startedAt) ?? '—'}</span>
        <Caps>Finished</Caps>
        <span className={clock(stageRun?.finishedAt) ? 'v' : 'v muted'}>{clock(stageRun?.finishedAt) ?? '—'}</span>
        <Caps>Duration</Caps>
        <span className={duration === null ? 'v muted' : 'v'}>{formatDuration(duration)}</span>
      </div>

      {stageRun?.error && <div className="inspector-error">{stageRun.error}</div>}
      {canTriage && aiTriage && stageRun?.status === 'failed' && pipelineId && runId && (
        <div className="field">
          <Caps>Triage</Caps>
          {triage ? (
            <>
              <p style={{ whiteSpace: 'pre-wrap' }}>{triage.advisory}</p>
              <span className="mono" style={{ fontSize: 12, color: 'var(--ink-3)' }}>
                {triage.model}
              </span>
            </>
          ) : (
            <div className="gate-actions">
              <button type="button" className="hud-btn" onClick={runTriage} disabled={triaging}>
                {triaging ? 'Analysing…' : 'Explain this failure'}
              </button>
            </div>
          )}
          {triageError && <div className="inspector-error">{triageError}</div>}
        </div>
      )}

      {awaiting && (
        <div className="field">
          <Caps>Approval gate</Caps>
          {gate && (
            <span className="v muted mono" style={{ fontSize: 12 }}>
              {gate.votes?.length ?? 0} of {gate.requiredApprovers} approvals
            </span>
          )}
          {gate?.votes && gate.votes.length > 0 && (
            <div className="gate-votes">
              {gate.votes.map((v) => (
                <span key={`${v.approverEmail}-${v.createdAt}`}>
                  ✓ {v.approverEmail}
                  {v.note ? ` — ${v.note}` : ''}
                </span>
              ))}
            </div>
          )}
          {gateState !== 'ready' && <p role="status">Gate information is {gateState === 'loading' ? 'being checked' : 'unavailable'}. Last loaded votes do not authorize a new decision.</p>}
          {!onGate && <p>An administrator or dedicated approver can decide this gate while the run is active.</p>}
          {onGate && <><label className="field"><Caps>Approval note (optional)</Caps><input className="input" value={note} onChange={(e) => setNote(e.target.value)} disabled={busy !== null} /></label>
          <div className="gate-actions">
            <button type="button" className="hud-btn hud-btn-primary" disabled={busy !== null || !canDecide} onClick={() => act('approve')}>
              {busy === 'approve' ? 'Approving…' : 'Approve'}
            </button>
            <button type="button" className="hud-btn" disabled={busy !== null || !canDecide} onClick={() => act('reject')}>
              {busy === 'reject' ? 'Rejecting…' : 'Reject'}
            </button>
          </div></>}
          {gateError && <p role="alert">{gateError}</p>}
        </div>
      )}
      {gate && !awaiting && gate.status !== 'awaiting' && (
        <div className="kv">
          <Caps>Gate</Caps>
          <span className="v">
            {gate.status}
            {gate.resolvedBy ? ` by ${gate.resolvedBy}` : ''}
          </span>
        </div>
      )}
      {!awaiting && gateError && <div className="field"><p role="alert">{gateError}</p><label className="field"><Caps>Retained approval note</Caps><input className="input" value={note} readOnly /></label></div>}

      {stageRun?.artifacts && stageRun.artifacts.length > 0 && (
        <div className="field">
          <Caps>Artifacts</Caps>
          <ul className="inspector-list">
            {stageRun.artifacts.map((a) => (
              <li key={`${a.type}-${a.ref}`} title={a.digest}>
                {a.type} · {a.ref}
              </li>
            ))}
          </ul>
        </div>
      )}

      {stageRun?.outputs && Object.keys(stageRun.outputs).length > 0 && (
        <div className="field">
          <Caps>Outputs</Caps>
          <div className="kv">
            {Object.entries(stageRun.outputs).map(([k, v]) => (
              <span key={k} style={{ display: 'contents' }}>
                <span className="v muted">{k}</span>
                <span className="v">{v}</span>
              </span>
            ))}
          </div>
        </div>
      )}

      {!appId && pipelineId && runId && (stage.type === 'deploy' || stageRun?.runtimeReceipt) && <NativeRuntimePanel subject={{ kind: 'native-stage', pipelineId, runId, stageId: stage.id }} receipt={stageRun?.runtimeReceipt} onStatus={onNativeRuntimeStatus} />}
      {service && (
        <div className="field">
          <Caps>Runtime · {service}</Caps>
          <p>Current recorded deployment service; this status is separate from the selected run’s outcome.</p>
          {runtime ? (
            <div className="kv">
              <Caps>State</Caps>
              <span className="v">{runtime.state}</span>
              <Caps>Healthy</Caps>
              <span className="v">{runtime.healthy ? 'yes' : 'no'}</span>
              {runtime.image && (
                <>
                  <Caps>Image</Caps>
                  <span className="v">{runtime.image}</span>
                </>
              )}
              {runtime.message && (
                <>
                  <Caps>Note</Caps>
                  <span className="v muted">{runtime.message}</span>
                </>
              )}
            </div>
          ) : (
            <span className="v muted mono" style={{ fontSize: 12 }}>
              status unavailable
            </span>
          )}
        </div>
      )}
    </aside>
  );
}
