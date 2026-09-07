import { memo, useContext, type CSSProperties } from 'react';
import { Handle, Position, type Node, type NodeProps } from '@xyflow/react';
import type { RunStatus, StageConfig, StageType } from '../../types/pipeline';
import { runSub } from './runState';
import { SceneContext } from './sceneContext';
import StageSymbol from '../pipeline/StageSymbol';
import { STAGE_LABELS, type StageKind } from '../pipeline/stageKinds';

export type StarStatus = 'idle' | RunStatus;

export interface StarData extends Record<string, unknown> {
  label: string;
  stageType: StageType;
  /** Compose services have their own symbol instead of impersonating custom stages. */
  kind?: StageKind;
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
 * A typed instrument inside a 48px star: distinct symbol, type label,
 * config/timing, and a separate status dot. Existing handle positions stay stable.
 */
function StarNode({ id, data, selected }: NodeProps<StarNodeType>) {
  const scene = useContext(SceneContext);
  const status = data.status ?? 'idle';
  const isSelected = selected || scene.selectedId === id;
  const cls = `star stage-node star-${status}${isSelected ? ' is-selected' : ''}`;
  const kind = data.kind ?? data.stageType;
  const typeLabel = STAGE_LABELS[kind] ?? 'Custom';
  const style = { '--draw-delay': `${data.drawDelay ?? 0}ms` } as CSSProperties;
  const sub = data.sub ?? runSub(data.stageType, data.config, status, data.startedAt, data.finishedAt, scene.now);
  return (
    <div className={cls} style={style} title={`${data.label} · ${typeLabel} · ${sub}`} data-stage-type={kind}>
      <Handle type="target" position={Position.Left} className="star-handle" />
      <span className="halo" aria-hidden="true" />
      <StageSymbol kind={kind} className="stage-symbol" />
      <span className="core" aria-hidden="true" />
      <span className="lbl">{data.label}</span>
      <span className="stage-kind">{typeLabel}</span>
      <span className="sub mono">{sub}</span>
      <Handle type="source" position={Position.Right} className="star-handle" />
    </div>
  );
}

export default memo(StarNode);
