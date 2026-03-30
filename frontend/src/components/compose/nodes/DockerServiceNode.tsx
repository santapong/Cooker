import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { ComposeService } from '../../../types/compose';

const statusColors: Record<string, string> = {
  running: '#22c55e',
  exited: '#ef4444',
  paused: '#f59e0b',
  unknown: '#475569',
};

export default function DockerServiceNode({ data }: NodeProps) {
  const service = data.service as ComposeService;
  const status = service.status || 'unknown';
  const imageLabel = service.image || (service.build ? `build: ${service.build.context}` : 'no image');
  const ports = service.ports?.length ? service.ports.join(', ') : null;

  return (
    <div style={{ ...styles.node, borderColor: statusColors[status] || '#475569' }}>
      <Handle type="target" position={Position.Left} style={styles.handle} />
      <div style={styles.header}>
        <span style={styles.icon}>D</span>
        <span style={styles.label}>{data.label as string}</span>
        <span style={{ ...styles.statusDot, backgroundColor: statusColors[status] || '#475569' }} />
      </div>
      <div style={styles.detail}>{imageLabel}</div>
      {ports && <div style={styles.ports}>{ports}</div>}
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
    minWidth: 200,
    color: '#f1f5f9',
    cursor: 'pointer',
  },
  header: { display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 },
  icon: {
    width: 24, height: 24, borderRadius: 4, backgroundColor: '#0ea5e9',
    color: 'white', display: 'flex', alignItems: 'center', justifyContent: 'center',
    fontSize: 12, fontWeight: 'bold',
  },
  label: { fontSize: 13, fontWeight: 600, flex: 1 },
  statusDot: {
    width: 8, height: 8, borderRadius: '50%',
  },
  detail: { fontSize: 11, color: '#94a3b8', marginTop: 4 },
  ports: {
    fontSize: 10, color: '#64748b', marginTop: 4,
    padding: '2px 6px', backgroundColor: '#0f172a', borderRadius: 3,
    display: 'inline-block',
  },
  handle: { width: 8, height: 8, backgroundColor: '#0ea5e9' },
};
