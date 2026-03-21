import type { PipelineRun } from '../../../types/pipeline';

const statusColors: Record<string, string> = {
  pending: '#475569',
  running: '#3b82f6',
  success: '#22c55e',
  failed: '#ef4444',
  cancelled: '#6b7280',
};

interface Props {
  runs: PipelineRun[];
  onSelectRun: (runId: string) => void;
}

export default function RunHistoryPanel({ runs, onSelectRun }: Props) {
  if (runs.length === 0) {
    return (
      <div style={styles.empty}>No runs yet. Click "Run" to execute the pipeline.</div>
    );
  }

  return (
    <div style={styles.panel}>
      <h3 style={styles.title}>Run History</h3>
      {runs.map((run) => (
        <div
          key={run.id}
          style={styles.runItem}
          onClick={() => onSelectRun(run.id)}
        >
          <span style={{ ...styles.statusDot, backgroundColor: statusColors[run.status] }} />
          <div>
            <div style={styles.runId}>{run.id.slice(0, 8)}</div>
            <div style={styles.runMeta}>{run.status} - {run.startedAt || 'queued'}</div>
          </div>
        </div>
      ))}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: 12 },
  title: { fontSize: 14, fontWeight: 600, color: '#f1f5f9', margin: '0 0 12px' },
  empty: { padding: 16, color: '#94a3b8', fontSize: 13, textAlign: 'center' },
  runItem: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '8px 10px',
    borderRadius: 4,
    cursor: 'pointer',
    marginBottom: 4,
    backgroundColor: '#0f172a',
  },
  statusDot: { width: 8, height: 8, borderRadius: '50%' },
  runId: { fontSize: 12, fontWeight: 600, color: '#f1f5f9' },
  runMeta: { fontSize: 11, color: '#94a3b8' },
};
