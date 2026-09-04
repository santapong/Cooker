import { memo, type CSSProperties } from 'react';
import { Handle, Position, type Node, type NodeProps } from '@xyflow/react';
import type { RunStatus, StageConfig, StageType } from '../../types/pipeline';
import { stageSub } from './constellation';

export type StarStatus = 'idle' | RunStatus;

export interface StarData extends Record<string, unknown> {
  label: string;
  stageType: StageType;
  config: StageConfig;
  environmentId?: string;
  /** Run state — colours the star and halo. Absent/idle in the editor. */
  status?: StarStatus;
  /** Draw-in delay (ms) for the scene entrance. */
  drawDelay?: number;
}

export type StarNodeType = Node<StarData>;

/**
 * A pipeline stage as a star: 6px core, 48px halo sprite, small-caps label
 * and a mono sub-label. Handles are invisible until hover/selection so the
 * constellation stays clean; drag from the right rim to connect.
 */
function StarNode({ data, selected }: NodeProps<StarNodeType>) {
  const status = data.status ?? 'idle';
  const cls = `star star-${status}${selected ? ' is-selected' : ''}`;
  const style = { '--draw-delay': `${data.drawDelay ?? 0}ms` } as CSSProperties;
  return (
    <div className={cls} style={style} title={`${data.label} · ${data.stageType}`} data-stage-type={data.stageType}>
      <Handle type="target" position={Position.Left} className="star-handle" />
      <span className="halo" aria-hidden="true" />
      <span className="core" aria-hidden="true" />
      <span className="lbl">{data.label}</span>
      <span className="sub mono">{stageSub(data.stageType, data.config)}</span>
      <Handle type="source" position={Position.Right} className="star-handle" />
    </div>
  );
}

export default memo(StarNode);
