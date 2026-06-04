import { getBezierPath, useNodes, type EdgeProps, type Node } from '@xyflow/react';
import { useTheme } from '../../../theme/ThemeProvider';
import { hexA } from '../../../theme/tokens';
import { statusTone } from '../../ui/atoms';
import { useReducedMotion } from '../../../hooks/useReducedMotion';

type TrajectoryState = 'idle' | 'active' | 'done';

// Edge state derives from the SOURCE node's run status — no new state.
// upstream succeeded → done · upstream running → active · else → idle.
function stateFromStatus(status?: string): TrajectoryState {
  const tone = statusTone(status);
  if (tone === 'good') return 'done';
  if (tone === 'ember') return 'active';
  return 'idle';
}

/**
 * ConditionalEdge — a "trajectory" between two planets. A cubic-bezier path
 * with a state-driven look: idle dotted, done green light-trail, active ember/
 * violet marching gradient with a travelling comet.
 */
export default function ConditionalEdge(props: EdgeProps) {
  const { id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, source, label } = props;
  const t = useTheme();
  const reduced = useReducedMotion();
  const nodes = useNodes();

  const sourceNode = nodes.find((n: Node) => n.id === source);
  const sourceStatus = (sourceNode?.data as { status?: string } | undefined)?.status;
  const state = stateFromStatus(sourceStatus);
  const active = state === 'active';
  const done = state === 'done';

  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
  });

  const gradId = `traj-${id}`;

  return (
    <>
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="1" y2="0">
          <stop offset="0%" stopColor={done ? t.good : active ? t.violetGlow : t.lineStrong} />
          <stop offset="100%" stopColor={done ? t.good : active ? t.ember : t.line} />
        </linearGradient>
      </defs>

      {/* soft blurred underlay glow for active/done */}
      {(active || done) && (
        <path
          d={edgePath}
          fill="none"
          stroke={active ? t.ember : t.good}
          strokeWidth={active ? 7 : 5}
          strokeLinecap="round"
          opacity={0.22}
          style={{ filter: 'blur(4px)' }}
        />
      )}

      {/* the trajectory itself */}
      <path
        className="react-flow__edge-path"
        d={edgePath}
        fill="none"
        stroke={`url(#${gradId})`}
        strokeWidth={active ? 2.4 : 1.6}
        strokeLinecap="round"
        strokeDasharray={state === 'idle' ? '1 7' : active ? '8 6' : 'none'}
        style={{ animation: active && !reduced ? 'ccDash 0.9s linear infinite' : 'none' }}
      />

      {/* travelling comet (decorative; skipped under reduced motion) */}
      {active && !reduced && (
        <circle r="3.6" fill="#fff" style={{ filter: `drop-shadow(0 0 5px ${t.ember})` }}>
          <animateMotion dur="1.8s" repeatCount="indefinite" path={edgePath} />
        </circle>
      )}

      {label && (
        <foreignObject
          x={labelX - 30}
          y={labelY - 10}
          width={60}
          height={20}
          requiredExtensions="http://www.w3.org/1999/xhtml"
        >
          <div
            style={{
              fontFamily: t.mono,
              fontSize: 9.5,
              color: t.textSoft,
              background: hexA(t.surfaceSolid, 0.85),
              padding: '2px 6px',
              borderRadius: 999,
              textAlign: 'center',
              border: `1px solid ${t.line}`,
            }}
          >
            {label}
          </div>
        </foreignObject>
      )}
    </>
  );
}
