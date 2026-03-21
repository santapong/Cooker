import { Handle, Position, type NodeProps } from '@xyflow/react';

const nodeColors: Record<string, string> = {
  pending: '#475569',
  running: '#3b82f6',
  success: '#22c55e',
  failed: '#ef4444',
};

export default function BuildNode({ data }: NodeProps) {
  const status = (data.status as string) || 'pending';

  return (
    <div style={{ ...styles.node, borderColor: nodeColors[status] || '#475569' }}>
      <Handle type="target" position={Position.Left} style={styles.handle} />
      <div style={styles.header}>
        <span style={styles.icon}>B</span>
        <span style={styles.label}>{data.label as string}</span>
      </div>
      <div style={styles.detail}>
        {data.config && (data.config as Record<string, string>).dockerfile
          ? `Dockerfile: ${(data.config as Record<string, string>).dockerfile}`
          : 'Configure build...'}
      </div>
      <Handle type="source" position={Position.Right} style={styles.handle} />
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  node: {
    padding: 12,
    borderRadius: 8,
    backgroundColor: '#1e293b',
    border: '2px solid #475569',
    minWidth: 180,
    color: '#f1f5f9',
  },
  header: { display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 },
  icon: {
    width: 24, height: 24, borderRadius: 4, backgroundColor: '#7c3aed',
    color: 'white', display: 'flex', alignItems: 'center', justifyContent: 'center',
    fontSize: 12, fontWeight: 'bold',
  },
  label: { fontSize: 13, fontWeight: 600 },
  detail: { fontSize: 11, color: '#94a3b8', marginTop: 4 },
  handle: { width: 8, height: 8, backgroundColor: '#3b82f6' },
};
