import { memo, useCallback, useEffect, useRef, useState, type DragEvent } from 'react';
import {
  ReactFlow,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Connection,
  type Edge,
  type EdgeTypes,
  type Node,
  type NodeTypes,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { usePipelineStore } from '../../stores/pipelineStore';
import { useMotionAllowed } from '../../hooks/useMotionAllowed';
import type { StageType } from '../../types/pipeline';
import StarNode from '../porthole/StarNode';
import ConstellationEdge from '../porthole/ConstellationEdge';
import { drawDelay, edgeDelay, SCENE_BUDGET_MS } from '../porthole/constellation';
import StageTray, { DRAG_MIME } from './StageTray';

// Every stage type is a star; the type shows in the sub-label.
const nodeTypes: NodeTypes = {
  build: StarNode,
  test: StarNode,
  push: StarNode,
  deploy: StarNode,
  approval: StarNode,
  custom: StarNode,
  'gitops-commit': StarNode,
};
const edgeTypes: EdgeTypes = { constellation: ConstellationEdge };

const STAR_HALF = 24;

function decorateNodes(nodes: Node[]): Node[] {
  return nodes.map((n, i) => ({ ...n, data: { ...n.data, drawDelay: drawDelay(i, nodes.length) } }));
}
function decorateEdges(edges: Edge[]): Edge[] {
  return edges.map((e, i) => ({ ...e, data: { ...e.data, drawDelay: edgeDelay(i) } }));
}
/** Store → canvas merge: definition wins, but a dragged node keeps its live position/selection. */
function mergeNodes(prev: Node[], next: Node[]): Node[] {
  const byId = new Map(prev.map((n) => [n.id, n]));
  return next.map((n) => {
    const p = byId.get(n.id);
    return p ? { ...n, position: p.position, selected: p.selected, measured: p.measured, dragging: p.dragging } : n;
  });
}

function starEl(id: string): HTMLElement | null {
  return document.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(id)}"] .star`);
}

/**
 * The porthole canvas: React Flow with StarNode/ConstellationEdge, seeded
 * from the pipeline store and kept in sync both ways. Positions persist on
 * drag stop; drag settle and new-star pop-in are WAAPI tweens gated by
 * useMotionAllowed (rung 5 — the only JS-driven motion in the editor).
 */
function PipelineCanvas() {
  const storeNodes = usePipelineStore((s) => s.nodes);
  const storeEdges = usePipelineStore((s) => s.edges);
  const connectStages = usePipelineStore((s) => s.connectStages);
  const addStage = usePipelineStore((s) => s.addStage);
  const removeStage = usePipelineStore((s) => s.removeStage);
  const removeEdge = usePipelineStore((s) => s.removeEdge);
  const moveStage = usePipelineStore((s) => s.moveStage);
  const setSelectedNode = usePipelineStore((s) => s.setSelectedNode);
  const selectedNodeId = usePipelineStore((s) => s.selectedNodeId);
  const cycleEdgeCondition = usePipelineStore((s) => s.cycleEdgeCondition);

  const motion = useMotionAllowed();
  const { screenToFlowPosition } = useReactFlow();
  const wrapper = useRef<HTMLDivElement>(null);

  const [nodes, setNodes, onNodesChange] = useNodesState(decorateNodes(storeNodes));
  const [edges, setEdges, onEdgesChange] = useEdgesState(decorateEdges(storeEdges));

  // Scene entrance: the draw-in classes apply for one scene budget, then
  // the constellation rests with no animations attached.
  const [entering, setEntering] = useState(true);
  useEffect(() => {
    const t = window.setTimeout(() => setEntering(false), SCENE_BUDGET_MS + 300);
    return () => window.clearTimeout(t);
  }, []);

  useEffect(() => {
    setNodes((prev) => mergeNodes(prev, decorateNodes(storeNodes)));
  }, [storeNodes, setNodes]);
  useEffect(() => {
    setEdges(decorateEdges(storeEdges));
  }, [storeEdges, setEdges]);
  // The store owns selection (inspector, tray adds); mirror it into React
  // Flow so the selected ring and the inspector never disagree.
  useEffect(() => {
    setNodes((prev) =>
      prev.some((n) => !!n.selected !== (n.id === selectedNodeId))
        ? prev.map((n) => ({ ...n, selected: n.id === selectedNodeId }))
        : prev,
    );
  }, [selectedNodeId, setNodes]);

  const popIn = useCallback(
    (id: string) => {
      requestAnimationFrame(() => {
        const el = starEl(id);
        if (!el) return;
        const frames = motion
          ? [{ opacity: 0, transform: 'scale(0.6)' }, { opacity: 1, transform: 'scale(1)' }]
          : [{ opacity: 0 }, { opacity: 1 }];
        el.animate(frames, { duration: motion ? 200 : 160, easing: 'cubic-bezier(.2,0,0,1)' });
      });
    },
    [motion],
  );

  const addAt = useCallback(
    (type: StageType, position: { x: number; y: number }) => {
      const id = addStage(type, { x: Math.round(position.x - STAR_HALF), y: Math.round(position.y - STAR_HALF) });
      if (id) {
        setSelectedNode(id);
        popIn(id);
      }
    },
    [addStage, popIn, setSelectedNode],
  );

  const addAtCentre = useCallback(
    (type: StageType) => {
      const box = wrapper.current?.getBoundingClientRect();
      if (!box) return;
      // Slight jitter so repeated clicks don't stack stars exactly.
      const jitter = (Math.random() - 0.5) * 80;
      addAt(type, screenToFlowPosition({ x: box.left + box.width / 2 + jitter, y: box.top + box.height / 2 + jitter }));
    },
    [addAt, screenToFlowPosition],
  );

  const onDragOver = useCallback((e: DragEvent) => {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
  }, []);
  const onDrop = useCallback(
    (e: DragEvent) => {
      e.preventDefault();
      const type = e.dataTransfer.getData(DRAG_MIME) as StageType;
      if (!type) return;
      addAt(type, screenToFlowPosition({ x: e.clientX, y: e.clientY }));
    },
    [addAt, screenToFlowPosition],
  );

  const onConnect = useCallback(
    (c: Connection) => {
      if (c.source && c.target && c.source !== c.target) connectStages(c.source, c.target);
    },
    [connectStages],
  );

  const onNodeDragStop = useCallback(
    (_: unknown, node: Node) => {
      moveStage(node.id, { x: Math.round(node.position.x), y: Math.round(node.position.y) });
      if (!motion) return; // reduced / calm: instant snap
      starEl(node.id)?.animate([{ transform: 'scale(1.08)' }, { transform: 'scale(1)' }], {
        duration: 160,
        easing: 'cubic-bezier(.2,0,0,1)',
      });
    },
    [moveStage, motion],
  );

  const onNodesDelete = useCallback((deleted: Node[]) => deleted.forEach((n) => removeStage(n.id)), [removeStage]);
  const onEdgesDelete = useCallback((deleted: Edge[]) => deleted.forEach((e) => removeEdge(e.id)), [removeEdge]);
  const onNodeClick = useCallback((_: unknown, node: Node) => setSelectedNode(node.id), [setSelectedNode]);
  const onPaneClick = useCallback(() => setSelectedNode(null), [setSelectedNode]);
  const onEdgeClick = useCallback((_: unknown, edge: Edge) => cycleEdgeCondition(edge.id), [cycleEdgeCondition]);

  return (
    <div ref={wrapper} className={entering ? 'canvas is-entering' : 'canvas'}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onNodeDragStop={onNodeDragStop}
        onNodesDelete={onNodesDelete}
        onEdgesDelete={onEdgesDelete}
        onNodeClick={onNodeClick}
        onPaneClick={onPaneClick}
        onEdgeClick={onEdgeClick}
        onDragOver={onDragOver}
        onDrop={onDrop}
        defaultEdgeOptions={{ type: 'constellation' }}
        fitView
        fitViewOptions={{ padding: 0.3, maxZoom: 1.1 }}
        minZoom={0.4}
        maxZoom={2}
        deleteKeyCode={['Backspace', 'Delete']}
        proOptions={{ hideAttribution: true }}
        style={{ background: 'transparent' }}
      />
      <StageTray onAdd={addAtCentre} />
    </div>
  );
}

export default memo(PipelineCanvas);
