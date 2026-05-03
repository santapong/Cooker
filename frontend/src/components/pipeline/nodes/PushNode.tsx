import type { NodeProps } from '@xyflow/react';
import StageNode from './StageNode';

export default function PushNode(props: NodeProps) {
  return (
    <StageNode
      {...props}
      kind="push"
      detailFromConfig={(c) => (c.registry ? String(c.registry) : undefined)}
      fallbackDetail="OCI registry"
    />
  );
}
