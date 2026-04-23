import { useCallback, useRef } from 'react';
import {
  ReactFlow,
  MiniMap,
  Controls,
  Background,
  BackgroundVariant,
  useNodesState,
  useEdgesState,
  addEdge,
  type Connection,
  type NodeTypes,
  type EdgeTypes,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import BuildNode from './nodes/BuildNode';
import TestNode from './nodes/TestNode';
import DeployNode from './nodes/DeployNode';
import PushNode from './nodes/PushNode';
import ApprovalNode from './nodes/ApprovalNode';
import CustomNode from './nodes/CustomNode';
import ConditionalEdge from './edges/ConditionalEdge';
import { usePipelineStore } from '../../stores/pipelineStore';
import type { StageType } from '../../types/pipeline';

const nodeTypes: NodeTypes = {
  build: BuildNode,
  test: TestNode,
  deploy: DeployNode,
  push: PushNode,
  approval: ApprovalNode,
  custom: CustomNode,
};

const edgeTypes: EdgeTypes = {
  conditional: ConditionalEdge,
};

export default function PipelineCanvas() {
  const reactFlowWrapper = useRef<HTMLDivElement>(null);
  const store = usePipelineStore();

  const [nodes, setNodes, onNodesChange] = useNodesState(store.nodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(store.edges);

  const onConnect = useCallback(
    (params: Connection) => {
      setEdges((eds) => addEdge({ ...params, type: 'conditional' }, eds));
      if (params.source && params.target) {
        store.connectStages(params.source, params.target);
      }
    },
    [setEdges, store],
  );

  const onDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const onDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault();
      const type = event.dataTransfer.getData('application/cooker-node') as StageType;
      if (!type) return;

      const bounds = reactFlowWrapper.current?.getBoundingClientRect();
      if (!bounds) return;

      const position = {
        x: event.clientX - bounds.left - 90,
        y: event.clientY - bounds.top - 25,
      };

      store.addStage(type, position);
      setNodes(usePipelineStore.getState().nodes);
    },
    [store, setNodes],
  );

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: { id: string }) => {
      store.setSelectedNode(node.id);
    },
    [store],
  );

  return (
    <div ref={reactFlowWrapper} style={{ flex: 1, height: '100%' }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onDragOver={onDragOver}
        onDrop={onDrop}
        onNodeClick={onNodeClick}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        defaultEdgeOptions={{ type: 'conditional' }}
        fitView
        style={{ backgroundColor: '#0f172a' }}
      >
        <Controls />

        <MiniMap
          nodeColor={() => '#3b82f6'}
          style={{ backgroundColor: '#1e293b' }}
        />
        <Background variant={BackgroundVariant.Dots} gap={16} size={1} color="#334155" />
      </ReactFlow>
    </div>
  );
}
