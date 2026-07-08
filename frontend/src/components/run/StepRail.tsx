import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import {
  Btn,
  Pill,
  StatusDot,
  statusTone,
  toneColor,
  type Tone,
} from '../ui/atoms';
import { Icon } from '../ui/Icon';
import { duration } from '../../utils/duration';
import StageGatePanel from './StageGatePanel';
import type { PipelineRun, Stage, StageApproval } from '../../types/pipeline';

export default function StepRail({
  run,
  stages,
  stageGates,
  selectedStageId,
  onSelect,
  canCancel,
  cancelling,
  onCancel,
  pipelineId,
  runId,
  onGateResolved,
}: {
  run: PipelineRun | null;
  stages: Stage[];
  stageGates: Record<string, StageApproval>;
  selectedStageId: string | null;
  onSelect: (id: string) => void;
  canCancel: boolean;
  cancelling: boolean;
  onCancel: () => void;
  pipelineId: string;
  runId: string;
  onGateResolved: () => void;
}) {
  const t = useTheme();
  const stageRunMap = new Map(run?.stageRuns?.map((s) => [s.stageId, s]) ?? []);
  return (
    <aside
      style={{
        borderRight: `1px solid ${t.line}`,
        background: t.surface,
        overflow: 'auto',
        padding: '16px 0',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div style={{ padding: '0 18px 14px' }}>
        <div
          style={{
            fontFamily: t.mono,
            fontSize: 10.5,
            letterSpacing: 1.2,
            textTransform: 'uppercase',
            color: t.textMute,
          }}
        >
          Run #{run?.id?.slice(0, 8) ?? '—'}
        </div>
        <div
          style={{
            fontFamily: t.serif,
            fontSize: 22,
            fontWeight: 500,
            color: t.text,
            marginTop: 2,
          }}
        >
          {run?.pipelineId?.slice(0, 12) ?? 'pipeline'}
        </div>
        <div style={{ display: 'flex', gap: 6, marginTop: 8, flexWrap: 'wrap' }}>
          <Pill tone={statusTone(run?.status)}>{run?.status ?? 'queued'}</Pill>
          {run?.startedAt && (
            <Pill>{new Date(run.startedAt).toLocaleTimeString()}</Pill>
          )}
        </div>
      </div>

      <div style={{ position: 'relative', padding: '4px 0', flex: 1 }}>
        <span
          style={{
            position: 'absolute',
            left: 32,
            top: 18,
            bottom: 18,
            width: 1,
            background: `linear-gradient(${t.good}, ${t.ember}, ${t.line})`,
          }}
        />
        {stages.map((s) => {
          const sr = stageRunMap.get(s.id);
          const gate = stageGates[s.id];
          const awaiting = gate?.status === 'awaiting';
          // An awaiting gate overrides the persisted "running" stage status
          // so the rail tints warn and reads "awaiting" while paused.
          const tone = awaiting ? 'warn' : statusTone(sr?.status);
          const isCurrent = tone === 'ember' || awaiting;
          const isSelected = selectedStageId === s.id;
          return (
            <div key={s.id}>
              <div
                onClick={() => onSelect(s.id)}
                style={{
                  display: 'grid',
                  gridTemplateColumns: '44px 1fr auto',
                  alignItems: 'center',
                  gap: 8,
                  padding: '10px 18px 10px 14px',
                  background: isSelected
                    ? hexA(t.accent, 0.06)
                    : isCurrent
                      ? hexA(t.ember, 0.06)
                      : 'transparent',
                  borderLeft: `3px solid ${isSelected ? t.accent : isCurrent ? t.ember : 'transparent'}`,
                  cursor: 'pointer',
                }}
              >
                <StepDot tone={tone} />
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12.5, fontWeight: 600, color: t.text }}>{s.name}</div>
                  <div
                    style={{
                      fontFamily: t.mono,
                      fontSize: 10.5,
                      color: t.textMute,
                      marginTop: 2,
                    }}
                  >
                    {s.type}
                  </div>
                </div>
                <span
                  style={{
                    fontFamily: t.mono,
                    fontSize: 11,
                    color: isCurrent ? t.ember : t.textMute,
                  }}
                >
                  {awaiting ? 'awaiting' : (duration(sr?.startedAt, sr?.finishedAt) ?? '—')}
                </span>
              </div>
              {awaiting && (
                <StageGatePanel
                  gate={gate}
                  pipelineId={pipelineId}
                  runId={runId}
                  stageId={s.id}
                  onResolved={onGateResolved}
                />
              )}
            </div>
          );
        })}
        {stages.length === 0 && (
          <div style={{ padding: '20px 18px', color: t.textMute, fontSize: 13 }}>
            This pipeline has no stages yet.
          </div>
        )}
      </div>

      <div
        style={{
          padding: '12px 18px',
          borderTop: `1px solid ${t.line}`,
          display: 'flex',
          gap: 8,
        }}
      >
        <Btn
          kind="danger"
          icon="close"
          onClick={onCancel}
          disabled={!canCancel || cancelling}
          style={{ flex: 1, justifyContent: 'center' }}
        >
          {cancelling ? 'Cancelling…' : 'Cancel run'}
        </Btn>
      </div>
    </aside>
  );
}

function StepDot({ tone }: { tone: Tone }) {
  const t = useTheme();
  const c = toneColor(t, tone);
  return (
    <div
      style={{
        width: 24,
        height: 24,
        borderRadius: 999,
        background: t.bg,
        border: `2px solid ${c}`,
        display: 'grid',
        placeItems: 'center',
        marginLeft: 6,
      }}
    >
      {tone === 'good' && <Icon name="check" size={12} style={{ color: c }} />}
      {tone === 'ember' && <StatusDot tone="ember" pulse size={8} />}
      {tone === 'bad' && <Icon name="close" size={12} style={{ color: c }} />}
      {(tone === 'neutral' || tone === 'warn') && (
        <span style={{ width: 6, height: 6, borderRadius: 999, background: t.textMute }} />
      )}
    </div>
  );
}
