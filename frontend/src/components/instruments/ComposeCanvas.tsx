import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ReactFlow, useNodesInitialized, type Edge, type EdgeTypes, type Node, type NodeChange, type NodeTypes } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import type { ComposeGraph } from '../../types/compose';
import StarNode from '../porthole/StarNode';
import ConstellationEdge from '../porthole/ConstellationEdge';
import { drawDelay, edgeDelay, SCENE_BUDGET_MS } from '../porthole/constellation';
import { layoutCompose } from './composeLayout';

const nodeTypes: NodeTypes = { service: StarNode };
const edgeTypes: EdgeTypes = { constellation: ConstellationEdge };

interface Props {
  graph: ComposeGraph;
  onSelect: (name: string | null) => void;
}

/** A compose file as a constellation: services are stars, depends_on / env / network links are the lines. */
function ComposeCanvas({ graph, onSelect }: Props) {
  const cacheRef = useRef(new Map<string, Node>());
  const initialized = useNodesInitialized();
  const [tick, setTick] = useState(0);
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    if (changes.some((c) => c.type === 'dimensions')) setTick((t) => t + 1);
  }, []);
  const nodes = useMemo<Node[]>(() => {
    const pos = layoutCompose(graph.services, graph.connections);
    const n = pos.length;
    return pos.map((p, i) => {
      const svc = graph.services.find((s) => s.name === p.name)!;
      const key = `${p.name}|${svc.image}|${p.x},${p.y}`;
      const hit = cacheRef.current.get(key);
      if (hit) return hit;
      const node: Node = {
        id: p.name,
        type: 'service',
        position: { x: p.x, y: p.y },
        draggable: false,
        connectable: false,
        data: { label: p.name, stageType: 'custom', config: {}, status: 'idle', sub: svc.image || 'build', drawDelay: drawDelay(i, n) },
      };
      cacheRef.current.set(key, node);
      return node;
    });
    // initialized / tick republish after React Flow measured the stars
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graph, initialized, tick]);
  const edges = useMemo<Edge[]>(
    () =>
      graph.connections.map((c, i) => ({
        id: `${c.source}-${c.target}-${c.type}`,
        source: c.source,
        target: c.target,
        type: 'constellation',
        selectable: false,
        // depends_on is the plain line; env references are dashed (the 'always' stroke) and named by the variable, networks by the network
        data: { condition: c.type === 'env_reference' ? 'always' : undefined, label: c.type === 'depends_on' ? undefined : c.label, state: 'idle', drawDelay: edgeDelay(i) },
      })),
    [graph],
  );
  const [entering, setEntering] = useState(true);
  useEffect(() => {
    const t = window.setTimeout(() => setEntering(false), SCENE_BUDGET_MS + 300);
    return () => window.clearTimeout(t);
  }, []);
  return (
    <div className={entering ? 'canvas is-entering' : 'canvas'}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesFocusable={false}
        onNodeClick={(_, node) => onSelect(node.id)}
        onPaneClick={() => onSelect(null)}
        fitView
        fitViewOptions={{ padding: 0.3, maxZoom: 1.1 }}
        minZoom={0.4}
        maxZoom={2}
        deleteKeyCode={null}
        proOptions={{ hideAttribution: true }}
        style={{ background: 'transparent' }}
      />
    </div>
  );
}

export default memo(ComposeCanvas);
