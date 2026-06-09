import { memo, useCallback } from 'react';
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

function ComposeCanvas() {
  const t = useTheme();
  // Action-only subscription: service-config edits rebuild the graph in
  // the store, and a whole-store subscription would re-render xyflow on
  // each one (P26-05-25). Local state is seeded once at mount — the
  // page gates mounting on a loaded graph and remounts on refresh.
  const setSelectedService = useComposeStore((s) => s.setSelectedService);
  const [nodes, , onNodesChange] = useNodesState(useComposeStore.getState().nodes);
  const [edges, , onEdgesChange] = useEdgesState(useComposeStore.getState().edges);
  const dark = t.mode === 'dark';

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: { id: string }) => {
      setSelectedService(node.id);
    },
    [setSelectedService],
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

// memo: parent page re-renders on store changes (it shows the service
// count); keep those renders from cascading into ReactFlow.
export default memo(ComposeCanvas);
