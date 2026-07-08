import { useCallback, useEffect, useState } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { useEnvironmentStore } from '../../stores/environmentStore';
import { useToastStore } from '../../stores/toastStore';
import { pipelineApi } from '../../api/pipelines';
import { Btn, Pill, SectionLabel, statusTone, type Tone } from '../ui/atoms';
import type { EnvironmentStatus, RunDiffReport } from '../../types/pipeline';

export default function RightRail({
  pipelineId,
  runId,
  envStatuses,
  onRefresh,
}: {
  pipelineId: string;
  runId: string;
  envStatuses: EnvironmentStatus[];
  onRefresh: () => void;
}) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const environments = useEnvironmentStore((s) => s.environments);
  const fetchEnvironments = useEnvironmentStore((s) => s.fetchEnvironments);
  const [busy, setBusy] = useState<string | null>(null);
  const [diff, setDiff] = useState<RunDiffReport | null>(null);

  useEffect(() => {
    if (environments.length === 0) fetchEnvironments();
  }, [environments.length, fetchEnvironments]);

  const loadDiff = useCallback(() => {
    if (!pipelineId || !runId) return;
    pipelineApi
      .getRunDiff(pipelineId, runId)
      .then(setDiff)
      .catch(() => setDiff(null));
  }, [pipelineId, runId]);

  useEffect(() => {
    loadDiff();
  }, [loadDiff]);

  const envName = (id: string) => environments.find((e) => e.id === id)?.name ?? id.slice(0, 8);

  const promote = async (toEnvId: string) => {
    setBusy(`promote:${toEnvId}`);
    try {
      await pipelineApi.promoteRun(pipelineId, runId, toEnvId);
      pushToast({ kind: 'success', message: `Promotion to ${envName(toEnvId)} requested.` });
      onRefresh();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(null);
    }
  };

  const approve = async (envId: string) => {
    setBusy(`approve:${envId}`);
    try {
      await pipelineApi.approvePromotion(pipelineId, runId, envId);
      pushToast({ kind: 'success', message: `Approved ${envName(envId)}.` });
      onRefresh();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(null);
    }
  };

  const orderedEnvs = environments.slice().sort((a, b) => a.order - b.order);

  return (
    <aside
      style={{
        borderLeft: `1px solid ${t.line}`,
        background: t.surface,
        overflow: 'auto',
        padding: '16px 18px',
        display: 'flex',
        flexDirection: 'column',
        gap: 18,
      }}
    >
      <div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
          <SectionLabel>Diff vs last green</SectionLabel>
          <span style={{ flex: 1 }} />
          <Btn onClick={loadDiff}>refresh</Btn>
        </div>
        {!diff || diff.reason ? (
          <div style={{ color: t.textMute, fontSize: 12, fontFamily: t.mono, fontStyle: 'italic' }}>
            {diff?.reason ?? 'no diff available'}
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
              <Pill tone="cool">vs {diff.againstRunId?.slice(0, 8)}</Pill>
              {diff.definitionChanged && (
                <Pill tone="warn">
                  definition v{diff.pipelineVersionDelta?.from}→v{diff.pipelineVersionDelta?.to}
                </Pill>
              )}
            </div>
            {diff.stages.map((s) => (
              <div
                key={s.stageId}
                style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 11.5 }}
              >
                <span
                  style={{
                    fontFamily: t.mono,
                    color: t.textSoft,
                    flex: 1,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {s.name || s.stageId}
                </span>
                {s.status.from !== s.status.to && (
                  <Pill tone={statusTone(s.status.to)}>
                    {s.status.from || '–'}→{s.status.to}
                  </Pill>
                )}
                {s.durationMs.from >= 0 && s.durationMs.to >= 0 && s.durationMs.deltaMs !== 0 && (
                  <span
                    style={{
                      fontFamily: t.mono,
                      fontSize: 10.5,
                      color: s.durationMs.deltaMs > 0 ? t.bad : t.good,
                    }}
                  >
                    {s.durationMs.deltaMs > 0 ? '+' : ''}
                    {(s.durationMs.deltaMs / 1000).toFixed(1)}s
                  </span>
                )}
                {s.digest.changed && <Pill tone="cool">digest</Pill>}
              </div>
            ))}
            {diff.variables && (
              <div style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
                vars: +{Object.keys(diff.variables.added ?? {}).length} −
                {Object.keys(diff.variables.removed ?? {}).length} ~
                {Object.keys(diff.variables.changed ?? {}).length}
              </div>
            )}
          </div>
        )}
      </div>

      <SectionLabel>Promotion path</SectionLabel>
      {orderedEnvs.length === 0 ? (
        <div style={{ color: t.textMute, fontSize: 12, fontFamily: t.mono, fontStyle: 'italic' }}>
          No environments configured.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {orderedEnvs.map((env) => {
            const status = envStatuses.find((s) => s.environmentId === env.id);
            const tone: Tone = status
              ? status.status === 'deployed'
                ? 'good'
                : status.status === 'failed'
                  ? 'bad'
                  : status.status === 'awaiting_approval'
                    ? 'warn'
                    : status.status === 'deploying' || status.status === 'approved'
                      ? 'ember'
                      : 'neutral'
              : 'neutral';
            const needsApproval = status?.status === 'awaiting_approval';
            const settled = status?.status === 'deployed' || status?.status === 'approved';
            return (
              <div
                key={env.id}
                style={{
                  padding: '10px 12px',
                  background: t.bg,
                  border: `1px solid ${t.line}`,
                  borderRadius: 8,
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 6,
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span style={{ fontFamily: t.mono, fontSize: 12, fontWeight: 600, color: t.text, flex: 1 }}>
                    {env.name}
                  </span>
                  {needsApproval && status?.approvalsNeed ? (
                    <Pill tone="warn">
                      {status.approvalsHave ?? 0}/{status.approvalsNeed} approved
                    </Pill>
                  ) : (
                    <Pill tone={tone}>{status?.status ?? 'pending'}</Pill>
                  )}
                </div>
                {status?.promotedAt && (
                  <div style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
                    promoted {new Date(status.promotedAt).toLocaleTimeString()}
                  </div>
                )}
                <div style={{ display: 'flex', gap: 6, marginTop: 4 }}>
                  {needsApproval && (
                    <Btn
                      kind="primary"
                      icon="check"
                      onClick={() => approve(env.id)}
                      disabled={busy !== null}
                      style={{ flex: 1, justifyContent: 'center' }}
                    >
                      {busy === `approve:${env.id}` ? 'Approving…' : 'Approve'}
                    </Btn>
                  )}
                  {!needsApproval && !settled && (
                    <Btn
                      kind="ink"
                      icon="arrow"
                      onClick={() => promote(env.id)}
                      disabled={busy !== null}
                      style={{ flex: 1, justifyContent: 'center' }}
                    >
                      {busy === `promote:${env.id}` ? 'Promoting…' : 'Promote here'}
                    </Btn>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}

      <SectionLabel>Live telemetry</SectionLabel>
      <div style={{ color: t.textMute, fontSize: 12, fontFamily: t.mono, fontStyle: 'italic' }}>
        Telemetry stream not wired up yet (follow-up: per-run WebSocket).
      </div>
    </aside>
  );
}
