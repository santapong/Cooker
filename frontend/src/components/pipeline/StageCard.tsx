import { memo, useEffect } from 'react';
import { Handle, Position, useUpdateNodeInternals, type Node, type NodeProps } from '@xyflow/react';
import type { StageType } from '../../types/pipeline';
import StageSymbol from './StageSymbol';
import { STAGE_LABELS } from './stageKinds';
import { portTop, requirementSummary, stageHeight } from './dependencies';

interface StageCardData extends Record<string, unknown> {
  label: string; stageType: StageType; incoming: string[]; outgoing: string[]; canEdit: boolean;
}
function StageCard({ id, data, selected }: NodeProps<Node<StageCardData>>) {
  const updateInternals = useUpdateNodeInternals();
  const incoming = data.incoming ?? []; const outgoing = data.outgoing ?? [];
  const height = stageHeight(Math.max(incoming.length, outgoing.length));
  const ports = `${incoming.join('|')}/${outgoing.join('|')}`;
  useEffect(() => { updateInternals(id); }, [id, ports, updateInternals]);
  return <div className={`pipeline-stage-card${selected ? ' is-selected' : ''}`} style={{ height }} data-stage-type={data.stageType}>
    <div className="pipeline-stage-kind"><StageSymbol kind={data.stageType} className="stage-symbol" /><span className="stage-kind">{STAGE_LABELS[data.stageType]}</span></div>
    <strong className="lbl" title={data.label}>{data.label}</strong>
    <span className="pipeline-stage-wait">{requirementSummary(incoming.length)}</span>
    {incoming.map((edge, index) => <Handle key={edge} id={edge} type="target" position={Position.Left} style={{ top: portTop(index, incoming.length, height) }} isConnectable={false} className="pipeline-port" />)}
    {outgoing.map((edge, index) => <Handle key={edge} id={edge} type="source" position={Position.Right} style={{ top: portTop(index, outgoing.length, height) }} isConnectable={false} className="pipeline-port" />)}
    {data.canEdit && <>
      <Handle id="new-target" type="target" position={Position.Left} style={{ top: 20 }} className="pipeline-port pipeline-port-new" title="Connect a prerequisite to this step" />
      <Handle id="new-source" type="source" position={Position.Right} style={{ top: 20 }} className="pipeline-port pipeline-port-new" title="Connect this step to the step that waits" />
    </>}
  </div>;
}
export default memo(StageCard);
