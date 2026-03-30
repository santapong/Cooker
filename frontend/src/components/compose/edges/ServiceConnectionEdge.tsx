import { BaseEdge, getSmoothStepPath, type EdgeProps } from '@xyflow/react';

const typeColors: Record<string, string> = {
  depends_on: '#3b82f6',
  env_reference: '#f97316',
  network: '#22c55e',
};

export default function ServiceConnectionEdge(props: EdgeProps) {
  const { sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, label, data } = props;
  const connectionType = (data?.connectionType as string) || 'depends_on';
  const color = typeColors[connectionType] || '#475569';

  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition,
  });

  return (
    <>
      <BaseEdge path={edgePath} style={{ stroke: color, strokeWidth: 2 }} />
      {label && (
        <foreignObject
          x={labelX - 40}
          y={labelY - 10}
          width={80}
          height={20}
          requiredExtensions="http://www.w3.org/1999/xhtml"
        >
          <div style={{
            fontSize: 9,
            color: color,
            backgroundColor: '#1e293b',
            padding: '2px 6px',
            borderRadius: 3,
            textAlign: 'center',
            border: `1px solid ${color}33`,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
          }}>
            {label}
          </div>
        </foreignObject>
      )}
    </>
  );
}
