import { BaseEdge, getBezierPath, type EdgeProps } from '@xyflow/react';
import { useTheme } from '../../../theme/ThemeProvider';
import { hexA } from '../../../theme/tokens';

export default function ServiceConnectionEdge(props: EdgeProps) {
  const { sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, label, data } = props;
  const t = useTheme();

  const typeColors: Record<string, string> = {
    depends_on: t.cool,
    env_reference: t.ember,
    network: t.good,
  };
  const connectionType = (data?.connectionType as string) || 'depends_on';
  const color = typeColors[connectionType] || t.textMute;

  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
  });

  return (
    <>
      <BaseEdge path={edgePath} style={{ stroke: color, strokeWidth: 1.8, strokeDasharray: '6 5' }} />
      {label && (
        <foreignObject
          x={labelX - 40}
          y={labelY - 10}
          width={80}
          height={20}
          requiredExtensions="http://www.w3.org/1999/xhtml"
        >
          <div
            style={{
              fontFamily: t.mono,
              fontSize: 9,
              color,
              background: hexA(t.surfaceSolid, 0.85),
              padding: '2px 6px',
              borderRadius: 999,
              textAlign: 'center',
              border: `1px solid ${hexA(color, 0.35)}`,
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
            }}
          >
            {label}
          </div>
        </foreignObject>
      )}
    </>
  );
}
