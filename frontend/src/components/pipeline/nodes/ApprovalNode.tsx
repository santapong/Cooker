import type { NodeProps } from '@xyflow/react';
import StageNode from './StageNode';

export default function ApprovalNode(props: NodeProps) {
  return <StageNode {...props} kind="approval" fallbackDetail="human in the loop" />;
}
