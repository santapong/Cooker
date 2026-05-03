import type { NodeProps } from '@xyflow/react';
import StageNode from './StageNode';

export default function TestNode(props: NodeProps) {
  return (
    <StageNode
      {...props}
      kind="test"
      detailFromConfig={(c) => (c.image ? `image · ${String(c.image)}` : undefined)}
      fallbackDetail="container test"
    />
  );
}
