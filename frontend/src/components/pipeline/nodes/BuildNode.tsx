import type { NodeProps } from '@xyflow/react';
import StageNode from './StageNode';

export default function BuildNode(props: NodeProps) {
  return (
    <StageNode
      {...props}
      kind="build"
      detailFromConfig={(c) =>
        c.dockerfile ? `Dockerfile · ${String(c.dockerfile)}` : undefined
      }
      fallbackDetail="Dockerfile · root"
    />
  );
}
