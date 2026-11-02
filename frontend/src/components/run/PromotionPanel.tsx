import { useCallback, useEffect, useRef, useState } from 'react';
import { pipelineApi } from '../../api/pipelines';
import { environmentsApi } from '../../api/environments';
import type { Environment } from '../../types/environment';
import type { EnvironmentStatus } from '../../types/pipeline';
import Badge, { type BadgeVariant } from '../ui/Badge';
import Caps from '../ui/Caps';
import ConfirmButton from '../ui/ConfirmButton';
import { pushToast } from '../../stores/toastStore';
import { timeAgo } from '../../utils/time';
import { useAuth } from '../../auth/OIDCProvider';
import { promotionDecisionMessage, readPromotionSnapshot, runRolePolicy } from './approvalPolicy';

function envVariant(status?: EnvironmentStatus['status']): BadgeVariant {
  switch (status) {
    case 'deployed':
    case 'approved':
      return 'ok';
    case 'failed':
      return 'fail';
    case 'deploying':
      return 'running';
    case 'awaiting_approval':
      return 'ember';
    default:
      return 'muted';
  }
}

interface Props {
  pipelineId: string;
  runId: string;
  terminal: boolean;
  onClose: () => void;
}

/** Environment lanes for a run: promote the artefact along Dev → Staging → Production, approve manual gates. */
export default function PromotionPanel({ pipelineId, runId, terminal, onClose }: Props) {
  const { canInvoke, canApprove } = runRolePolicy(useAuth().user?.roles);
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [statuses, setStatuses] = useState<EnvironmentStatus[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const coordinates = useRef({ pipelineId, runId });
  const refreshRequest = useRef(0);
  const lifecycle = useRef(0);
  coordinates.current = { pipelineId, runId };

  const invalidate = useCallback(() => {
    refreshRequest.current++;
    lifecycle.current++;
  }, []);
  const refresh = useCallback(async () => {
    const request = ++refreshRequest.current;
    const current = () => request === refreshRequest.current && coordinates.current.pipelineId === pipelineId && coordinates.current.runId === runId;
    setLoading(true);
    const [e, s] = await Promise.allSettled([environmentsApi.list({ limit: 100 }), pipelineApi.promotionSnapshot(pipelineId, runId)]);
    if (!current()) return;
    const snapshot = e.status === 'fulfilled' && s.status === 'fulfilled' ? readPromotionSnapshot(e.value, s.value, runId) : null;
    if (snapshot) {
      setEnvs([...snapshot.environments].sort((a, b) => a.order - b.order));
      setStatuses(snapshot.statuses);
      setError(null);
    } else setError('Promotion information could not be loaded. Refresh before recording a decision.');
    setLoading(false);
  }, [pipelineId, runId]);
  useEffect(() => {
    setBusy(null);
    setEnvs([]);
    setStatuses([]);
    setError(null);
    void refresh();
    const timer = terminal ? undefined : window.setInterval(() => void refresh(), 5000);
    return () => {
      invalidate();
      if (timer !== undefined) window.clearInterval(timer);
    };
  }, [refresh, terminal, invalidate]);

  const act = async (key: string, fn: () => Promise<{ status: string }>, kind: 'approve' | 'promote') => {
    if (loading || error || kind === 'approve' && !canApprove || kind === 'promote' && !canInvoke) return;
    const generation = lifecycle.current;
    const current = () => generation === lifecycle.current && coordinates.current.pipelineId === pipelineId && coordinates.current.runId === runId;
    setBusy(key);
    try {
      const result = await fn();
      if (!current()) return;
      const confirmation = promotionDecisionMessage(result?.status, kind);
      if (!confirmation) throw new Error('Promotion decision unconfirmed.');
      pushToast('success', confirmation);
      await refresh();
    } catch {
      if (!current()) return;
      pushToast('error', 'This promotion decision could not be confirmed. Refresh before retrying.');
    } finally {
      if (current()) setBusy(null);
    }
  };

  return (
    <aside className="inspector" aria-label="Environment promotion">
      <div className="inspector-head">
        <Badge variant="muted">promotion</Badge>
        <span className="spacer" />
        <button type="button" className="inspector-close" onClick={onClose} aria-label="Close promotion panel">
          ×
        </button>
      </div>
      <h2>Environments</h2>
      <p>These records describe promotion policy and approval progress. Approval does not confirm a completed target deployment.</p>
      {error && <div className="inspector-error" role="alert">{error}</div>}
      {(error || loading) && envs.length > 0 && <p>Last loaded promotion records remain visible while current information is unavailable or being checked. They do not authorize a new decision.</p>}
      {loading && <p role="status">Checking promotion status…</p>}
      <button type="button" className="hud-btn" disabled={busy !== null || loading} onClick={() => void refresh()}>Refresh promotion status</button>
      {envs.length === 0 && !loading && !error && <p>No environments configured yet.</p>}
      <div className="gate-votes">
        {envs.map((env) => {
          const st = statuses.find((s) => s.environmentId === env.id);
          const manual = env.promotion?.strategy === 'manual';
          return (
            <div key={env.id} className="field" style={{ paddingBottom: 10, borderBottom: '1px solid var(--line)' }}>
              <div className="inspector-status">
                <Caps>{env.name}</Caps>
                <Badge variant={envVariant(st?.status)}>{st?.status ?? 'not promoted'}</Badge>
              </div>
              <span className="mono" style={{ fontSize: 12, color: 'var(--ink-3)' }}>
                lane {env.order} · {manual ? `manual · ${env.promotion.requiredApprovers ?? 1} approvers` : 'auto'}
                {st?.promotedAt ? ` · promoted ${timeAgo(st.promotedAt)}` : ''}
                {st?.approvalsNeed ? ` · ${st.approvalsHave ?? 0}/${st.approvalsNeed} approvals` : ''}
                {st?.approvedBy ? ` · by ${st.approvedBy}` : ''}
              </span>
              <div className="gate-actions">
                {canApprove && !error && st?.status === 'awaiting_approval' && (
                  <ConfirmButton className="hud-btn hud-btn-primary" confirmLabel={`Approve ${env.name}?`} disabled={busy !== null || loading} onConfirm={() => act(env.id, () => pipelineApi.approvePromotion(pipelineId, runId, env.id), 'approve')}>
                    Approve
                  </ConfirmButton>
                )}
                {canInvoke && !error && (!st || st.status === 'failed') && (
                  <ConfirmButton className="hud-btn" confirmLabel={`Promote to ${env.name}?`} disabled={busy !== null || loading} onConfirm={() => act(env.id, () => pipelineApi.promoteRun(pipelineId, runId, env.id), 'promote')}>
                    Promote here
                  </ConfirmButton>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </aside>
  );
}
