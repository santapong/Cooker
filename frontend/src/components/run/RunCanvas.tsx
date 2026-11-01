import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ReactFlow,
  useNodesInitialized,
  useReactFlow,
  type Edge,
  type EdgeTypes,
  type Node,
  type NodeChange,
  type NodeTypes,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import type { Pipeline, PipelineRun } from '../../types/pipeline';
import { useMotionAllowed } from '../../hooks/useMotionAllowed';
import StarNode from '../porthole/StarNode';
import ConstellationEdge from '../porthole/ConstellationEdge';
import { drawDelay, edgeDelay, SCENE_BUDGET_MS } from '../porthole/constellation';
import { edgeStateFor, stageRunMap, starStatusFor } from '../porthole/runState';

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

interface Props {
  pipeline: Pipeline;
  run: PipelineRun | null;
  onSelect: (id: string | null) => void;
  /** Changes when the surrounding layout changes (console open/close) so the view refits. */
  layoutKey: string;
}

/**
 * Read-only constellation of a run: stars coloured by stage status, edges
 * lit as light passes, the comet riding the hot edge. Selection drives the
 * inspector and the console.
 */
function RunCanvas({ pipeline, run, onSelect, layoutKey }: Props) {
  const motion = useMotionAllowed();
  const { fitView } = useReactFlow();

  const byId = useMemo(() => stageRunMap(run), [run]);
  // Nodes are recomputed every tick (durations), but React Flow re-measures
  // a star — and briefly loses its edges — whenever it receives a *new*
  // node object. So node objects are cached by id and reused while nothing
  // about the star changed; only a running star (its duration ticks) gets a
  // fresh object each second.
  const cacheRef = useRef(new Map<string, { key: string; node: Node }>());
  // React Flow measures stars after mount, but with controlled nodes the
  // edges (which need the measured handle bounds) only render once the node
  // array is sent again. The very first measurement fires before React Flow
  // has registered our change handler (child effects run first), so the
  // republish is keyed on `useNodesInitialized` for the mount and on
  // dimension changes afterwards. Cached objects are reused, so nothing is
  // re-measured and the loop closes after one pass.
  const initialized = useNodesInitialized();
  const [measureTick, setMeasureTick] = useState(0);
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    if (changes.some((c) => c.type === 'dimensions')) setMeasureTick((t) => t + 1);
  }, []);
  const nodes = useMemo<Node[]>(() => {
    const n = pipeline.stages.length;
    const cache = cacheRef.current;
    const next = new Map<string, { key: string; node: Node }>();
    const out = pipeline.stages.map((s, i) => {
      const sr = byId.get(s.id);
      const status = starStatusFor(s.id, byId);
      const delay = drawDelay(i, n);
      const key = `${s.name}|${s.type}|${status}|${sr?.startedAt ?? ''}|${sr?.finishedAt ?? ''}|${s.position.x},${s.position.y}|${delay}`;
      const hit = cache.get(s.id);
      const node: Node =
        hit && hit.key === key
          ? hit.node
          : {
              id: s.id,
              type: s.type,
              position: s.position,
              draggable: false,
              connectable: false,
              data: {
                label: s.name,
                stageType: s.type,
                config: s.config,
                status,
                startedAt: sr?.startedAt ?? null,
                finishedAt: sr?.finishedAt ?? null,
                drawDelay: delay,
              },
            };
      next.set(s.id, { key, node });
      return node;
    });
    cacheRef.current = next;
    return out;
    // initialized / measureTick republish the (cached) objects after React Flow measured them
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pipeline, byId, initialized, measureTick]);
  const edges = useMemo<Edge[]>(
    () =>
      pipeline.edges.map((e, i) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        type: 'constellation',
        selectable: false,
        data: { condition: e.condition, state: edgeStateFor(e, byId), drawDelay: edgeDelay(i) },
      })),
    [pipeline, byId],
  );

  const [entering, setEntering] = useState(true);
  useEffect(() => {
    const t = window.setTimeout(() => setEntering(false), SCENE_BUDGET_MS + 300);
    return () => window.clearTimeout(t);
  }, []);

  const first = useState(true);
  useEffect(() => {
    if (first[0]) {
      first[1](false);
      return;
    }
    const t = window.setTimeout(() => fitView({ padding: 0.3, maxZoom: 1.1, duration: motion ? 200 : 0 }), 30);
    return () => window.clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layoutKey]);

  const onNodeClick = useCallback((_: unknown, node: Node) => onSelect(node.id), [onSelect]);
  const onPaneClick = useCallback(() => onSelect(null), [onSelect]);

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
        onNodeClick={onNodeClick}
        onPaneClick={onPaneClick}
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

export default memo(RunCanvas);
