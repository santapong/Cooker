import type { NodeProps } from '@xyflow/react';
import StageNode from './StageNode';

export default function CustomNode(props: NodeProps) {
  return <StageNode {...props} kind="custom" fallbackDetail="custom shell" />;
}
