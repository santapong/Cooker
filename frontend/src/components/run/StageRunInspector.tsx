import { useEffect, useState } from 'react';
import type { Stage, StageApproval, StageRun } from '../../types/pipeline';
import { runtimeApi, type ServiceRuntimeStatus } from '../../api/pipelines';
import Badge from '../ui/Badge';
import Caps from '../ui/Caps';
import { formatDuration, stageDurationMs, statusVariant } from '../porthole/runState';

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
  /** Deployment view: the app whose compose service this stage deployed. */
  appId?: string;
}

/** Right-hand inspector for a stage of a run: timing, error, artifacts, outputs, approval gate, runtime. */
export default function StageRunInspector({ stage, stageRun, gate, now, onClose, onGate, appId }: Props) {
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState<'approve' | 'reject' | null>(null);
  const [runtime, setRuntime] = useState<ServiceRuntimeStatus | null>(null);
  const service = stage.config.composeServiceName;

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
  const awaiting = stage.type === 'approval' && stageRun?.status === 'running' && (!gate || gate.status === 'awaiting');

  const act = async (kind: 'approve' | 'reject') => {
    if (!onGate) return;
    setBusy(kind);
    try {
      await onGate(kind, note);
      setNote('');
    } finally {
      setBusy(null);
    }
  };

  return (
    <aside className="inspector" aria-label={`Stage ${stage.name}`}>
      <div className="inspector-head">
        <div className="inspector-status">
          <Badge variant="muted">{stage.type}</Badge>
          <Badge variant={statusVariant(stageRun?.status)}>{stageRun?.status ?? 'not run'}</Badge>
        </div>
        <span className="spacer" />
        <button type="button" className="inspector-close" onClick={onClose} aria-label="Close inspector">
          ×
        </button>
      </div>
      <h2>{stage.name}</h2>

      <div className="kv">
        <Caps>Started</Caps>
        <span className={clock(stageRun?.startedAt) ? 'v' : 'v muted'}>{clock(stageRun?.startedAt) ?? '—'}</span>
        <Caps>Finished</Caps>
        <span className={clock(stageRun?.finishedAt) ? 'v' : 'v muted'}>{clock(stageRun?.finishedAt) ?? '—'}</span>
        <Caps>Duration</Caps>
        <span className={duration === null ? 'v muted' : 'v'}>{formatDuration(duration)}</span>
      </div>

      {stageRun?.error && <div className="inspector-error">{stageRun.error}</div>}

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
          <input className="input" value={note} placeholder="Note (optional)" onChange={(e) => setNote(e.target.value)} />
          <div className="gate-actions">
            <button type="button" className="hud-btn hud-btn-primary" disabled={busy !== null} onClick={() => act('approve')}>
              {busy === 'approve' ? 'Approving…' : 'Approve'}
            </button>
            <button type="button" className="hud-btn" disabled={busy !== null} onClick={() => act('reject')}>
              {busy === 'reject' ? 'Rejecting…' : 'Reject'}
            </button>
          </div>
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

      {service && (
        <div className="field">
          <Caps>Runtime · {service}</Caps>
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
