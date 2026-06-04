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
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Starfield } from '../ui/Starfield';

const nodeTypes: NodeTypes = {
  dockerService: DockerServiceNode,
};

const edgeTypes: EdgeTypes = {
  serviceConnection: ServiceConnectionEdge,
};

export default function ComposeCanvas() {
  const t = useTheme();
  const store = useComposeStore();
  const [nodes, , onNodesChange] = useNodesState(store.nodes);
  const [edges, , onEdgesChange] = useEdgesState(store.edges);
  const dark = t.mode === 'dark';

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: { id: string }) => {
      store.setSelectedService(node.id);
    },
    [store],
  );

  return (
    <div
      style={{
        flex: 1,
        height: '100%',
        position: 'relative',
        overflow: 'hidden',
        background: `radial-gradient(120% 100% at 50% -20%, ${t.canvasTop} 0%, ${t.canvasBot} 100%)`,
      }}
    >
      <Starfield seed={11} density={dark ? 80 : 20} nebula />
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
          nodeColor={() => t.cyan}
          maskColor={hexA(t.void, 0.7)}
          style={{
            background: t.panelGlass,
            border: `1px solid ${t.line}`,
            borderRadius: 10,
          }}
        />
        <Background variant={BackgroundVariant.Dots} gap={34} size={1} color={hexA(t.violet, 0.1)} />
      </ReactFlow>
    </div>
  );
}
