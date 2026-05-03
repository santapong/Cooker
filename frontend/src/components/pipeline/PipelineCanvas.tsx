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
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
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
  const t = useTheme();
  const reactFlowWrapper = useRef<HTMLDivElement>(null);
  const store = usePipelineStore();

  const [nodes, setNodes, onNodesChange] = useNodesState(store.nodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(store.edges);

  const onConnect = useCallback(
    (params: Connection) => {
      setEdges((eds) =>
        addEdge(
          {
            ...params,
            type: 'conditional',
            style: { stroke: t.textMute, strokeWidth: 1.6 },
          },
          eds,
        ),
      );
      if (params.source && params.target) {
        store.connectStages(params.source, params.target);
      }
    },
    [setEdges, store, t.textMute],
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
        x: event.clientX - bounds.left - 100,
        y: event.clientY - bounds.top - 30,
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
        defaultEdgeOptions={{
          type: 'conditional',
          style: { stroke: t.textMute, strokeWidth: 1.6 },
        }}
        fitView
        proOptions={{ hideAttribution: true }}
        style={{ background: t.bg }}
      >
        <Controls
          style={{
            background: t.surface,
            border: `1px solid ${t.line}`,
            borderRadius: 8,
            color: t.textSoft,
          }}
        />
        <MiniMap
          nodeColor={() => t.accent}
          maskColor={hexA(t.bg, 0.7)}
          style={{
            background: t.surface,
            border: `1px solid ${t.line}`,
            borderRadius: 8,
          }}
        />
        <Background variant={BackgroundVariant.Dots} gap={22} size={1} color={hexA(t.text, 0.08)} />
      </ReactFlow>
    </div>
  );
}
