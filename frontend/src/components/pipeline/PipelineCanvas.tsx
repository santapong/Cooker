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
import { Starfield } from '../ui/Starfield';
import { planetKindOf } from '../ui/atoms';
import type { Node } from '@xyflow/react';
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
      // the trajectory's look is derived inside ConditionalEdge from the
      // source node's run status — no per-edge style needed here.
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

  const onEdgeClick = useCallback(
    (event: React.MouseEvent, edge: { id: string }) => {
      // Cycle the trajectory's condition: (success) → failure → always.
      event.stopPropagation();
      store.cycleEdgeCondition(edge.id);
      setEdges(usePipelineStore.getState().edges);
    },
    [store, setEdges],
  );

  const dark = t.mode === 'dark';

  return (
    <div
      ref={reactFlowWrapper}
      style={{
        flex: 1,
        height: '100%',
        position: 'relative',
        overflow: 'hidden',
        background: `radial-gradient(120% 100% at 50% -20%, ${t.canvasTop} 0%, ${t.canvasBot} 100%)`,
      }}
    >
      {/* drifting starfield behind the graph */}
      <Starfield seed={42} density={dark ? 90 : 22} nebula />
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onDragOver={onDragOver}
        onDrop={onDrop}
        onNodeClick={onNodeClick}
        onEdgeClick={onEdgeClick}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        defaultEdgeOptions={{ type: 'conditional' }}
        fitView
        proOptions={{ hideAttribution: true }}
        style={{ background: 'transparent' }}
      >
        <Controls
          style={{
            background: t.panelGlass,
            border: `1px solid ${t.line}`,
            borderRadius: 10,
            color: t.textSoft,
            backdropFilter: 'blur(10px)',
          }}
        />
        <MiniMap
          nodeColor={(n: Node) => t.planets[planetKindOf(n.type)].glow}
          maskColor={hexA(t.void, 0.7)}
          style={{
            background: t.panelGlass,
            border: `1px solid ${t.line}`,
            borderRadius: 10,
          }}
        />
        {/* faint dotted grid (violet-tinted), pans with the canvas */}
        <Background variant={BackgroundVariant.Dots} gap={34} size={1} color={hexA(t.violet, 0.1)} />
      </ReactFlow>
    </div>
  );
}
