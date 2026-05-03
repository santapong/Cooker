import type { PipelineRun } from '../../../types/pipeline';
import { useTheme } from '../../../theme/ThemeProvider';
import { Pill, SectionLabel, StatusDot, statusTone } from '../../ui/atoms';

interface Props {
  runs: PipelineRun[];
  onSelectRun: (runId: string) => void;
}

export default function RunHistoryPanel({ runs, onSelectRun }: Props) {
  const t = useTheme();

  if (runs.length === 0) {
    return (
      <div
        style={{
          padding: 18,
          color: t.textMute,
          fontSize: 13,
          textAlign: 'center',
          fontStyle: 'italic',
        }}
      >
        No runs yet — hit Run pipeline to fire one off.
      </div>
    );
  }

  return (
    <div style={{ padding: '14px 16px', display: 'flex', flexDirection: 'column', gap: 10 }}>
      <SectionLabel>Run history</SectionLabel>
      {runs.map((run) => {
        const tone = statusTone(run.status);
        return (
          <div
            key={run.id}
            onClick={() => onSelectRun(run.id)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '10px 12px',
              borderRadius: 8,
              cursor: 'pointer',
              background: t.bg,
              border: `1px solid ${t.line}`,
            }}
          >
            <StatusDot tone={tone} pulse={tone === 'ember'} />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div
                style={{
                  fontFamily: t.mono,
                  fontSize: 12,
                  fontWeight: 600,
                  color: t.text,
                }}
              >
                #{run.id.slice(0, 8)}
              </div>
              <div style={{ fontSize: 11, color: t.textMute, marginTop: 2 }}>
                {run.startedAt ? new Date(run.startedAt).toLocaleString() : 'queued'}
              </div>
            </div>
            <Pill tone={tone}>{run.status}</Pill>
          </div>
        );
      })}
    </div>
  );
}
