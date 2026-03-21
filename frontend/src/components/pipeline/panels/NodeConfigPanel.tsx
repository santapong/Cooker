import { usePipelineStore } from '../../../stores/pipelineStore';

export default function NodeConfigPanel() {
  const { pipeline, selectedNodeId, updateStageConfig, removeStage, setSelectedNode } = usePipelineStore();

  if (!pipeline || !selectedNodeId) return null;

  const stage = pipeline.stages.find((s) => s.id === selectedNodeId);
  if (!stage) return null;

  return (
    <div style={styles.panel}>
      <div style={styles.header}>
        <h3 style={styles.title}>{stage.name}</h3>
        <button style={styles.closeBtn} onClick={() => setSelectedNode(null)}>x</button>
      </div>

      <div style={styles.section}>
        <label style={styles.label}>Type</label>
        <div style={styles.badge}>{stage.type}</div>
      </div>

      {stage.type === 'build' && (
        <>
          <div style={styles.section}>
            <label style={styles.label}>Dockerfile</label>
            <input
              style={styles.input}
              value={stage.config.dockerfile || ''}
              onChange={(e) => updateStageConfig(stage.id, { dockerfile: e.target.value })}
              placeholder="./Dockerfile"
            />
          </div>
          <div style={styles.section}>
            <label style={styles.label}>Context</label>
            <input
              style={styles.input}
              value={stage.config.context || ''}
              onChange={(e) => updateStageConfig(stage.id, { context: e.target.value })}
              placeholder="."
            />
          </div>
        </>
      )}

      {stage.type === 'test' && (
        <div style={styles.section}>
          <label style={styles.label}>Image</label>
          <input
            style={styles.input}
            value={stage.config.image || ''}
            onChange={(e) => updateStageConfig(stage.id, { image: e.target.value })}
            placeholder="node:20-alpine"
          />
        </div>
      )}

      {stage.type === 'deploy' && (
        <div style={styles.section}>
          <label style={styles.label}>Namespace</label>
          <input
            style={styles.input}
            value={stage.config.namespace || ''}
            onChange={(e) => updateStageConfig(stage.id, { namespace: e.target.value })}
            placeholder="default"
          />
        </div>
      )}

      {stage.type === 'push' && (
        <div style={styles.section}>
          <label style={styles.label}>Registry</label>
          <input
            style={styles.input}
            value={stage.config.registry || ''}
            onChange={(e) => updateStageConfig(stage.id, { registry: e.target.value })}
            placeholder="ghcr.io"
          />
        </div>
      )}

      <button
        className="btn-danger"
        style={{ marginTop: 16, width: '100%' }}
        onClick={() => removeStage(stage.id)}
      >
        Remove Stage
      </button>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  panel: {
    width: 280,
    backgroundColor: '#1e293b',
    borderLeft: '1px solid #334155',
    padding: 16,
    overflowY: 'auto',
  },
  header: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 },
  title: { fontSize: 14, fontWeight: 600, color: '#f1f5f9', margin: 0 },
  closeBtn: { background: 'none', border: 'none', color: '#94a3b8', fontSize: 16, cursor: 'pointer', padding: 4 },
  section: { marginBottom: 12 },
  label: { display: 'block', fontSize: 11, color: '#94a3b8', marginBottom: 4, textTransform: 'uppercase' as const },
  input: {
    width: '100%',
    padding: '6px 10px',
    borderRadius: 4,
    border: '1px solid #475569',
    backgroundColor: '#0f172a',
    color: '#f1f5f9',
    fontSize: 13,
    outline: 'none',
  },
  badge: {
    display: 'inline-block',
    padding: '2px 8px',
    borderRadius: 4,
    backgroundColor: '#334155',
    color: '#3b82f6',
    fontSize: 12,
    fontWeight: 600,
  },
};
