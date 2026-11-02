import { memo, useId, type CSSProperties } from 'react';
import { EdgeLabelRenderer, type Edge, type EdgeProps } from '@xyflow/react';
import type { PipelineEdge } from '../../types/pipeline';
import { useMotionAllowed } from '../../hooks/useMotionAllowed';
import { edgeMidpoint, edgePath } from './constellation';
import type { DependencyDrawing } from '../pipeline/dependencies';
import './dependency-edge.css';

export interface ConstellationData extends Record<string, unknown> {
  condition?: PipelineEdge['condition'];
  /** Free-text mid-line label (compose links: the env variable or network); shown instead of the condition word. */
  label?: string;
  /** Run state of the edge: light has passed (done) or is passing (hot). */
  state?: 'idle' | 'done' | 'hot';
  drawDelay?: number;
  /** Opt-in authored-pipeline view; Compose links keep their own semantics. */
  dependency?: DependencyDrawing;
  explanation?: string;
  zoom?: number;
  onInspect?: () => void;
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
  const marker = `dependency-${useId().replace(/:/g, '')}`;
  if (data?.dependency) {
    const dependency = data.dependency;
    const displaced = dependency.label.x !== dependency.anchor.x || dependency.label.y !== dependency.anchor.y;
    return <>
      <g className={`pipeline-dependency${selected ? ' is-selected' : ''} pipeline-dependency-${data.condition ?? 'success'}`}>
        <defs><marker id={marker} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="9" markerHeight="9" orient="auto-start-reverse"><path d="M 1 1 L 9 5 L 1 9 Z" /></marker></defs>
        <path d={dependency.path} className="pipeline-dependency-hit" />
        <path d={dependency.path} className="pipeline-dependency-line" markerEnd={`url(#${marker})`} />
        {displaced && <path d={`M ${dependency.anchor.x} ${dependency.anchor.y} L ${dependency.label.x} ${dependency.label.y}`} className="pipeline-dependency-leader" />}
      </g>
      <EdgeLabelRenderer><button type="button" className="pipeline-dependency-number nodrag nopan" style={{ transform: `translate(-50%, -50%) translate(${dependency.label.x}px, ${dependency.label.y}px) scale(${1 / (data.zoom ?? 1)})` }} aria-label={`Dependency ${dependency.number}: ${data.explanation}`} aria-pressed={!!selected} onClick={data.onInspect}><span>{dependency.number}</span></button></EdgeLabelRenderer>
    </>;
  }
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
