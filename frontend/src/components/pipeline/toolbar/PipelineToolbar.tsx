import type { StageType } from '../../../types/pipeline';

const stageTypes: { type: StageType; label: string; color: string }[] = [
  { type: 'build', label: 'Build', color: '#7c3aed' },
  { type: 'test', label: 'Test', color: '#0891b2' },
  { type: 'push', label: 'Push', color: '#d97706' },
  { type: 'deploy', label: 'Deploy', color: '#059669' },
  { type: 'approval', label: 'Approval', color: '#f59e0b' },
  { type: 'custom', label: 'Custom', color: '#6b7280' },
];

interface Props {
  onSave: () => void;
  onValidate: () => void;
  onRun: () => void;
}

export default function PipelineToolbar({ onSave, onValidate, onRun }: Props) {
  const onDragStart = (event: React.DragEvent, nodeType: string) => {
    event.dataTransfer.setData('application/cooker-node', nodeType);
    event.dataTransfer.effectAllowed = 'move';
  };

  return (
    <div style={styles.toolbar}>
      <div style={styles.section}>
        <span style={styles.sectionLabel}>Drag to add:</span>
        <div style={styles.nodeTypes}>
          {stageTypes.map((st) => (
            <div
              key={st.type}
              draggable
              onDragStart={(e) => onDragStart(e, st.type)}
              style={{ ...styles.nodeChip, borderColor: st.color }}
            >
              <span style={{ ...styles.chipIcon, backgroundColor: st.color }}>
                {st.label[0]}
              </span>
              {st.label}
            </div>
          ))}
        </div>
      </div>
      <div style={styles.actions}>
        <button className="btn-secondary" onClick={onValidate}>Validate</button>
        <button className="btn-primary" onClick={onSave}>Save</button>
        <button className="btn-success" onClick={onRun}>Run</button>
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '8px 16px',
    backgroundColor: '#1e293b',
    borderBottom: '1px solid #334155',
    gap: 16,
  },
  section: { display: 'flex', alignItems: 'center', gap: 12 },
  sectionLabel: { fontSize: 12, color: '#94a3b8', whiteSpace: 'nowrap' },
  nodeTypes: { display: 'flex', gap: 6 },
  nodeChip: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    padding: '4px 8px',
    borderRadius: 6,
    border: '1px solid',
    backgroundColor: '#0f172a',
    color: '#f1f5f9',
    fontSize: 12,
    cursor: 'grab',
    userSelect: 'none' as const,
  },
  chipIcon: {
    width: 18,
    height: 18,
    borderRadius: 3,
    color: 'white',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 10,
    fontWeight: 'bold',
  },
  actions: { display: 'flex', gap: 8 },
};
