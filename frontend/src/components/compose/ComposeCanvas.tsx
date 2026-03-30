import { useCallback } from 'react';
import {
  ReactFlow,
  MiniMap,
  Controls,
  Background,
  BackgroundVariant,
  useNodesState,
  useEdgesState,
  type NodeTypes,
  type EdgeTypes,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import DockerServiceNode from './nodes/DockerServiceNode';
import ServiceConnectionEdge from './edges/ServiceConnectionEdge';
import { useComposeStore } from '../../stores/composeStore';

const nodeTypes: NodeTypes = {
  dockerService: DockerServiceNode,
};

const edgeTypes: EdgeTypes = {
  serviceConnection: ServiceConnectionEdge,
};

export default function ComposeCanvas() {
  const store = useComposeStore();
  const [nodes, , onNodesChange] = useNodesState(store.nodes);
  const [edges, , onEdgesChange] = useEdgesState(store.edges);

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: { id: string }) => {
      store.setSelectedService(node.id);
    },
    [store],
  );

  return (
    <div style={{ flex: 1, height: '100%' }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        nodesDraggable
        nodesConnectable={false}
        fitView
        style={{ backgroundColor: '#0f172a' }}
      >
        <Controls />
        <MiniMap
          nodeColor={() => '#0ea5e9'}
          style={{ backgroundColor: '#1e293b' }}
        />
        <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="#334155" />
      </ReactFlow>
    </div>
  );
}
