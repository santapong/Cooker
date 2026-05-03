import type { NodeProps } from '@xyflow/react';
import StageNode from './StageNode';

export default function DeployNode(props: NodeProps) {
  return (
    <StageNode
      {...props}
      kind="deploy"
      detailFromConfig={(c) => (c.namespace ? `ns · ${String(c.namespace)}` : undefined)}
      fallbackDetail="Kubernetes"
    />
  );
}
