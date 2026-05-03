import { useTheme } from '../../../theme/ThemeProvider';
import { Btn, KindBadge, Pill, SectionLabel } from '../../ui/atoms';
import { hexA } from '../../../theme/tokens';
import type { StageType } from '../../../types/pipeline';

interface StageDef {
  type: StageType;
  label: string;
  desc: string;
}

const stageTypes: StageDef[] = [
  { type: 'build', label: 'Build image', desc: 'Docker / Buildx' },
  { type: 'test', label: 'Run tests', desc: 'go / npm / pytest' },
  { type: 'push', label: 'Push to registry', desc: 'OCI distribution' },
  { type: 'deploy', label: 'Deploy', desc: 'Kubernetes' },
  { type: 'approval', label: 'Approval gate', desc: 'human in the loop' },
  { type: 'custom', label: 'Custom shell', desc: 'any container' },
];

interface Props {
  onSave: () => void;
  onValidate: () => void;
  onRun: () => void;
}

export default function PipelineToolbar({ onSave, onValidate, onRun }: Props) {
  const t = useTheme();
  const onDragStart = (event: React.DragEvent, nodeType: string) => {
    event.dataTransfer.setData('application/cooker-node', nodeType);
    event.dataTransfer.effectAllowed = 'move';
  };

  return (
    <aside
      style={{
        width: 240,
        flexShrink: 0,
        borderRight: `1px solid ${t.line}`,
        background: t.surface,
        padding: '18px 14px',
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
        overflow: 'auto',
      }}
    >
      <SectionLabel>Add step</SectionLabel>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        {stageTypes.map((s) => (
          <div
            key={s.type}
            draggable
            onDragStart={(e) => onDragStart(e, s.type)}
            style={{
              padding: '10px 12px',
              border: `1px solid ${t.line}`,
              background: t.bg,
              borderRadius: 8,
              cursor: 'grab',
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              userSelect: 'none',
            }}
          >
            <KindBadge kind={s.type} />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 12.5, fontWeight: 600, color: t.text }}>{s.label}</div>
              <div
                style={{
                  fontFamily: t.mono,
                  fontSize: 10,
                  color: t.textMute,
                  marginTop: 2,
                }}
              >
                {s.desc}
              </div>
            </div>
          </div>
        ))}
      </div>

      <div style={{ height: 8 }} />
      <SectionLabel>Templates</SectionLabel>
      {['Go service · k8s', 'Node site · static', 'Worker · Redis'].map((s) => (
        <div
          key={s}
          style={{
            padding: '8px 12px',
            fontSize: 12,
            color: t.textSoft,
            cursor: 'pointer',
            borderRadius: 6,
            background: hexA(t.line, 0.15),
          }}
        >
          {s}
        </div>
      ))}

      <div style={{ flex: 1 }} />

      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 8,
          paddingTop: 12,
          borderTop: `1px solid ${t.line}`,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Pill tone="neutral">draft</Pill>
          <span style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
            unsaved · ⌘S
          </span>
        </div>
        <Btn kind="secondary" onClick={onValidate}>
          Validate DAG
        </Btn>
        <Btn kind="ink" onClick={onSave}>
          Save pipeline
        </Btn>
        <Btn kind="primary" icon="play" onClick={onRun}>
          Run pipeline
        </Btn>
      </div>
    </aside>
  );
}
