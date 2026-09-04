import { useCallback, useEffect, useState } from 'react';
import { pipelineApi } from '../../api/pipelines';
import { environmentsApi } from '../../api/environments';
import type { Environment } from '../../types/environment';
import type { EnvironmentStatus } from '../../types/pipeline';
import Badge, { type BadgeVariant } from '../ui/Badge';
import Caps from '../ui/Caps';
import ConfirmButton from '../ui/ConfirmButton';
import { pushToast } from '../../stores/toastStore';
import { timeAgo } from '../../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

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
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [statuses, setStatuses] = useState<EnvironmentStatus[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    const [e, s] = await Promise.allSettled([environmentsApi.list({ limit: 100 }), pipelineApi.envStatus(pipelineId, runId)]);
    if (e.status === 'fulfilled') setEnvs([...(e.value ?? [])].sort((a, b) => a.order - b.order));
    if (s.status === 'fulfilled') {
      setStatuses(s.value ?? []);
      setError(null);
    } else setError(message(s.reason));
  }, [pipelineId, runId]);
  useEffect(() => {
    void refresh();
    if (terminal) return;
    const t = window.setInterval(() => void refresh(), 5000);
    return () => window.clearInterval(t);
  }, [refresh, terminal]);

  const act = async (key: string, fn: () => Promise<unknown>, ok: string) => {
    setBusy(key);
    try {
      await fn();
      pushToast('success', ok);
      await refresh();
    } catch (e) {
      pushToast('error', message(e));
    } finally {
      setBusy(null);
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
      {error && <div className="inspector-error">{error}</div>}
      {envs.length === 0 && <p>No environments configured yet.</p>}
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
                {st?.status === 'awaiting_approval' && (
                  <ConfirmButton className="hud-btn hud-btn-primary" confirmLabel={`Approve ${env.name}?`} disabled={busy !== null} onConfirm={() => act(env.id, () => pipelineApi.approvePromotion(pipelineId, runId, env.id), `${env.name} approved.`)}>
                    Approve
                  </ConfirmButton>
                )}
                {(!st || st.status === 'failed') && (
                  <ConfirmButton className="hud-btn" confirmLabel={`Promote to ${env.name}?`} disabled={busy !== null} onConfirm={() => act(env.id, () => pipelineApi.promoteRun(pipelineId, runId, env.id), `Promotion to ${env.name} started.`)}>
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
