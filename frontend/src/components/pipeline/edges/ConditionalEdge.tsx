import { BaseEdge, getSmoothStepPath, type EdgeProps } from '@xyflow/react';

export default function ConditionalEdge(props: EdgeProps) {
  const { sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, label, style } = props;

  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition,
  });

  return (
    <>
      <BaseEdge path={edgePath} style={style} />
      {label && (
        <foreignObject
          x={labelX - 30}
          y={labelY - 10}
          width={60}
          height={20}
          requiredExtensions="http://www.w3.org/1999/xhtml"
        >
          <div style={{
            fontSize: 10,
            color: '#94a3b8',
            backgroundColor: '#1e293b',
            padding: '2px 6px',
            borderRadius: 3,
            textAlign: 'center',
            border: '1px solid #334155',
          }}>
            {label}
          </div>
        </foreignObject>
      )}
    </>
  );
}
