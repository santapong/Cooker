import { forwardRef, useMemo, type CSSProperties } from 'react';
import type { PipelineEdge, RunStatus, Stage } from '../../types/pipeline';
import { layoutMini } from './miniConstellation';

interface Props {
  stages: Stage[];
  edges: PipelineEdge[];
  /** Per-stage run status — colours the stars like the porthole does. */
  statuses?: Map<string, RunStatus> | Record<string, RunStatus>;
  width?: number;
  height?: number;
  className?: string;
  style?: CSSProperties;
  title?: string;
}

/**
 * 72×40 constellation thumbnail drawn from the pipeline's real stages and
 * edges. Doubles as the shared element that flies into the porthole.
 */
const MiniConstellation = forwardRef<SVGSVGElement, Props>(function MiniConstellation(
  { stages, edges, statuses, width = 72, height = 40, className, style, title },
  ref,
) {
  const layout = useMemo(() => layoutMini(stages, edges, width, height), [stages, edges, width, height]);
  const statusOf = (id: string): RunStatus | undefined =>
    statuses instanceof Map ? statuses.get(id) : statuses?.[id];
  return (
    <svg
      ref={ref}
      className={className ? `mini ${className}` : 'mini'}
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      style={style}
      role={title ? 'img' : undefined}
      aria-hidden={title ? undefined : true}
      focusable="false"
    >
      {title && <title>{title}</title>}
      {layout.segments.map((s) => (
        <line key={s.id} className="mini-edge" x1={s.x1} y1={s.y1} x2={s.x2} y2={s.y2} />
      ))}
      {layout.points.map((p) => {
        const st = statusOf(p.id);
        return <circle key={p.id} className={st ? `dot st-${st}` : 'dot'} cx={p.x} cy={p.y} r={st === 'running' ? 2.6 : 2} />;
      })}
    </svg>
  );
});

export default MiniConstellation;
