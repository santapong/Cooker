import { useState } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Btn, Pill } from '../ui/atoms';
import { useToastStore } from '../../stores/toastStore';
import { pipelineApi } from '../../api/pipelines';
import type { StageApproval } from '../../types/pipeline';

// StageGatePanel renders the Approve/Reject affordance for a paused
// approval-gate stage (StageTypeApproval). It posts to the stage
// approve/reject endpoints and, on success, asks the parent to refresh the
// run so the stage flips to succeeded/failed. RBAC is enforced server-side
// (admin-or-approver); a forbidden click surfaces as an error toast.
export default function StageGatePanel({
  gate,
  pipelineId,
  runId,
  stageId,
  onResolved,
}: {
  gate: StageApproval;
  pipelineId: string;
  runId: string;
  stageId: string;
  onResolved: () => void;
}) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [busy, setBusy] = useState<'approve' | 'reject' | null>(null);

  const act = async (kind: 'approve' | 'reject') => {
    setBusy(kind);
    try {
      if (kind === 'approve') await pipelineApi.approveStage(pipelineId, runId, stageId);
      else await pipelineApi.rejectStage(pipelineId, runId, stageId);
      pushToast({
        kind: 'success',
        message: kind === 'approve' ? 'Approval recorded.' : 'Stage rejected.',
      });
      onResolved();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(null);
    }
  };

  const have = gate.votes?.length ?? 0;
  const need = gate.requiredApprovers || 1;

  return (
    <div
      style={{
        margin: '0 18px 8px 44px',
        padding: '8px 10px',
        background: hexA(t.warn, 0.08),
        border: `1px solid ${hexA(t.warn, 0.35)}`,
        borderRadius: 8,
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <Pill tone="warn">awaiting approval</Pill>
        <span style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
          {have}/{need} approved
        </span>
      </div>
      <div style={{ display: 'flex', gap: 6 }}>
        <Btn
          kind="primary"
          icon="check"
          onClick={() => act('approve')}
          disabled={busy !== null}
          style={{ flex: 1, justifyContent: 'center' }}
        >
          {busy === 'approve' ? 'Approving…' : 'Approve'}
        </Btn>
        <Btn
          kind="danger"
          icon="close"
          onClick={() => act('reject')}
          disabled={busy !== null}
          style={{ flex: 1, justifyContent: 'center' }}
        >
          {busy === 'reject' ? 'Rejecting…' : 'Reject'}
        </Btn>
      </div>
    </div>
  );
}
