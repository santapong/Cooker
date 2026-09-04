import { memo, type CSSProperties } from 'react';
import type { Edge, EdgeProps } from '@xyflow/react';
import type { PipelineEdge } from '../../types/pipeline';
import { edgeMidpoint, edgePath } from './constellation';

export interface ConstellationData extends Record<string, unknown> {
  condition?: PipelineEdge['condition'];
  /** Run state of the edge: light has passed (done) or is passing (hot). */
  state?: 'idle' | 'done' | 'hot';
  drawDelay?: number;
}

export type ConstellationEdgeType = Edge<ConstellationData>;

/**
 * A constellation line between two stars: 1.5px, slightly curved. Drawn in
 * with stroke-dashoffset on scene entrance (pathLength normalises to 1).
 * A 20px transparent twin carries the pointer hits.
 */
function ConstellationEdge({ sourceX, sourceY, targetX, targetY, data, selected }: EdgeProps<ConstellationEdgeType>) {
  const d = edgePath(sourceX, sourceY, targetX, targetY);
  const cls = [
    'constellation',
    data?.condition ? `cond-${data.condition}` : '',
    data?.state && data.state !== 'idle' ? `is-${data.state}` : '',
    selected ? 'is-selected' : '',
  ]
    .filter(Boolean)
    .join(' ');
  const style = { '--draw-delay': `${data?.drawDelay ?? 0}ms` } as CSSProperties;
  const mid = data?.condition ? edgeMidpoint(sourceX, sourceY, targetX, targetY) : null;
  return (
    <g className={cls} style={style}>
      <path d={d} className="constellation-hit" />
      <path d={d} className="constellation-path" pathLength={1} />
      {mid && (
        <text className="constellation-cond" x={mid.x} y={mid.y - 6}>
          {data?.condition}
        </text>
      )}
    </g>
  );
}

export default memo(ConstellationEdge);
