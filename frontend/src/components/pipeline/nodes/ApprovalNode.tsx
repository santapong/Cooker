import { Handle, Position, type NodeProps } from '@xyflow/react';

export default function ApprovalNode({ data }: NodeProps) {
  const status = (data.status as string) || 'pending';
  const colors: Record<string, string> = { pending: '#475569', running: '#f59e0b', success: '#22c55e', failed: '#ef4444' };

  return (
    <div style={{ padding: 12, borderRadius: 8, backgroundColor: '#1e293b', border: `2px solid ${colors[status] || '#475569'}`, minWidth: 180, color: '#f1f5f9' }}>
      <Handle type="target" position={Position.Left} style={{ width: 8, height: 8, backgroundColor: '#3b82f6' }} />
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
        <span style={{ width: 24, height: 24, borderRadius: 4, backgroundColor: '#f59e0b', color: 'white', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 12, fontWeight: 'bold' }}>A</span>
        <span style={{ fontSize: 13, fontWeight: 600 }}>{data.label as string}</span>
      </div>
      <div style={{ fontSize: 11, color: '#f59e0b' }}>Manual approval gate</div>
      <Handle type="source" position={Position.Right} style={{ width: 8, height: 8, backgroundColor: '#3b82f6' }} />
    </div>
  );
}
