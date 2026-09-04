import { memo, type CSSProperties } from 'react';
import type { Edge, EdgeProps } from '@xyflow/react';
import type { PipelineEdge } from '../../types/pipeline';
import { useMotionAllowed } from '../../hooks/useMotionAllowed';
import { edgeMidpoint, edgePath } from './constellation';

export interface ConstellationData extends Record<string, unknown> {
  condition?: PipelineEdge['condition'];
  /** Free-text mid-line label (compose links: the env variable or network); shown instead of the condition word. */
  label?: string;
  /** Run state of the edge: light has passed (done) or is passing (hot). */
  state?: 'idle' | 'done' | 'hot';
  drawDelay?: number;
}

export type ConstellationEdgeType = Edge<ConstellationData>;

/**
 * A constellation line between two stars: 1.5px, slightly curved. Drawn in
 * with stroke-dashoffset on scene entrance (pathLength normalises to 1).
 * A 20px transparent twin carries the pointer hits. While the edge is hot
 * (light passing to a running stage) a comet rides the path — SMIL
 * animateMotion, 1.2 s per edge, which CSS media queries cannot reach, so
 * it is gated by useMotionAllowed: reduced motion / Calm show the static
 * brighter stroke instead (spec §3 substitution table).
 */
function ConstellationEdge({ sourceX, sourceY, targetX, targetY, data, selected }: EdgeProps<ConstellationEdgeType>) {
  const d = edgePath(sourceX, sourceY, targetX, targetY);
  const motion = useMotionAllowed();
  const comet = data?.state === 'hot' && motion;
  const cls = [
    'constellation',
    data?.condition ? `cond-${data.condition}` : '',
    data?.state && data.state !== 'idle' ? `is-${data.state}` : '',
    selected ? 'is-selected' : '',
  ]
    .filter(Boolean)
    .join(' ');
  const style = { '--draw-delay': `${data?.drawDelay ?? 0}ms` } as CSSProperties;
  const text = data?.label ?? data?.condition;
  const mid = text ? edgeMidpoint(sourceX, sourceY, targetX, targetY) : null;
  return (
    <g className={cls} style={style}>
      <path d={d} className="constellation-hit" />
      <path d={d} className="constellation-path" pathLength={1} />
      {mid && (
        <text className="constellation-cond" x={mid.x} y={mid.y - 6}>
          {text}
        </text>
      )}
      {comet && (
        <g className="comet" aria-hidden="true">
          <circle r="7" className="comet-glow" />
          <circle r="2.5" className="comet-core" />
          <animateMotion dur="1.2s" repeatCount="indefinite" path={d} calcMode="spline" keySplines="0.42 0 0.58 1" keyTimes="0;1" />
        </g>
      )}
    </g>
  );
}

export default memo(ConstellationEdge);
