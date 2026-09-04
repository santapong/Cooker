import { memo, useContext, type CSSProperties } from 'react';
import { Handle, Position, type Node, type NodeProps } from '@xyflow/react';
import type { RunStatus, StageConfig, StageType } from '../../types/pipeline';
import { runSub } from './runState';
import { SceneContext } from './sceneContext';

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
  /** Mono sub-label override. Defaults to a run duration/state when timing is present, else the config fact. */
  sub?: string;
  /** Run timing (run view) — the duration is derived against SceneContext.now. */
  startedAt?: string | null;
  finishedAt?: string | null;
}

export type StarNodeType = Node<StarData>;

/**
 * A pipeline stage as a star: 6px core, 48px halo sprite, small-caps label
 * and a mono sub-label. Handles are invisible until hover/selection so the
 * constellation stays clean; drag from the right rim to connect.
 */
function StarNode({ id, data, selected }: NodeProps<StarNodeType>) {
  const scene = useContext(SceneContext);
  const status = data.status ?? 'idle';
  const isSelected = selected || scene.selectedId === id;
  const cls = `star star-${status}${isSelected ? ' is-selected' : ''}`;
  const style = { '--draw-delay': `${data.drawDelay ?? 0}ms` } as CSSProperties;
  const sub = data.sub ?? runSub(data.stageType, data.config, status, data.startedAt, data.finishedAt, scene.now);
  return (
    <div className={cls} style={style} title={`${data.label} · ${data.stageType}`} data-stage-type={data.stageType}>
      <Handle type="target" position={Position.Left} className="star-handle" />
      <span className="halo" aria-hidden="true" />
      <span className="core" aria-hidden="true" />
      <span className="lbl">{data.label}</span>
      <span className="sub mono">{sub}</span>
      <Handle type="source" position={Position.Right} className="star-handle" />
    </div>
  );
}

export default memo(StarNode);
