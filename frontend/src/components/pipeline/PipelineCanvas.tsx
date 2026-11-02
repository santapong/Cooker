import { memo, useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react';
import { ReactFlow, useNodesInitialized, useNodesState, useReactFlow, useViewport, type Connection, type Edge, type EdgeTypes, type Node, type NodeTypes } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { usePipelineStore } from '../../stores/pipelineStore';
import { useMotionAllowed } from '../../hooks/useMotionAllowed';
import type { PipelineEdge, StageType } from '../../types/pipeline';
import ConstellationEdge from '../porthole/ConstellationEdge';
import StageCard from './StageCard';
import StageInspector from './StageInspector';
import StageTray, { DRAG_MIME } from './StageTray';
import { connectionProblem, dependencyDrawings, dependencyStory, dependencyViewport, nodeBounds, requirementSummary, STAGE_WIDTH } from './dependencies';
import './pipeline.css';

const nodeTypes: NodeTypes = Object.fromEntries(['build', 'test', 'push', 'deploy', 'approval', 'custom', 'gitops-commit'].map((type) => [type, StageCard]));
const edgeTypes: EdgeTypes = { constellation: ConstellationEdge };
const stageTypes: StageType[] = ['build', 'test', 'push', 'deploy', 'approval', 'custom'];

function PipelineCanvas({ canEdit }: { canEdit: boolean }) {
  const pipeline = usePipelineStore((state) => state.pipeline)!;
  const storeNodes = usePipelineStore((state) => state.nodes);
  const selectedNodeId = usePipelineStore((state) => state.selectedNodeId);
  const { connectStages, addStage, removeStage, removeEdge, moveStage, setSelectedNode, cycleEdgeCondition } = usePipelineStore();
  const [nodes, setNodes, onNodesChange] = useNodesState(storeNodes);
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null);
  const [mode, setMode] = useState<'dag' | 'list'>('dag');
  const [feedback, setFeedback] = useState('');
  const [mapWidth, setMapWidth] = useState(1000);
  const map = useRef<HTMLDivElement>(null);
  const focusAfterView = useRef<string | null>(null);
  const initialized = useNodesInitialized();
  const { screenToFlowPosition, setViewport, getNodes, setCenter, zoomIn, zoomOut } = useReactFlow();
  const { zoom } = useViewport();
  const motion = useMotionAllowed();

  useEffect(() => {
    setNodes((previous) => {
      const live = new Map(previous.map((node) => [node.id, node]));
      return storeNodes.map((node) => ({ ...node, position: live.get(node.id)?.position ?? node.position, measured: live.get(node.id)?.measured }));
    });
  }, [storeNodes, setNodes]);

  const selectStage = useCallback((id: string | null) => { setSelectedEdgeId(null); setSelectedNode(id); }, [setSelectedNode]);
  const selectEdge = useCallback((id: string) => { setSelectedNode(null); setSelectedEdgeId(id); }, [setSelectedNode]);
  const stages = useMemo(() => pipeline.stages.map((stage) => ({ ...stage, position: nodes.find((node) => node.id === stage.id)?.position ?? stage.position })), [pipeline.stages, nodes]);
  const drawing = useMemo(() => new Map(dependencyDrawings(stages, pipeline.edges, zoom, mapWidth * 0.78).map((edge) => [edge.id, edge])), [stages, pipeline.edges, zoom, mapWidth]);
  const flowNodes = useMemo(() => nodes.filter((node) => stages.some((stage) => stage.id === node.id)).map((node) => {
    const stage = stages.find((item) => item.id === node.id)!;
    const size = nodeBounds(stage, pipeline.edges);
    return { ...node, selected: node.id === selectedNodeId, draggable: canEdit, connectable: canEdit, ariaLabel: `${stage.name}. ${requirementSummary(pipeline.edges.filter((edge) => edge.target === node.id).length)}`, style: { width: size.width, height: size.height }, data: { ...node.data, incoming: pipeline.edges.filter((edge) => edge.target === node.id).map((edge) => edge.id), outgoing: pipeline.edges.filter((edge) => edge.source === node.id).map((edge) => edge.id), canEdit } };
  }), [nodes, stages, pipeline.edges, selectedNodeId, canEdit]);
  const edges = useMemo<Edge[]>(() => pipeline.edges.map((edge) => ({ ...edge, sourceHandle: edge.id, targetHandle: edge.id, type: 'constellation', selected: edge.id === selectedEdgeId, ariaLabel: dependencyStory(edge, stages).summary, data: { condition: edge.condition, dependency: drawing.get(edge.id), explanation: dependencyStory(edge, stages).summary, zoom, onInspect: () => selectEdge(edge.id) } })), [pipeline.edges, selectedEdgeId, stages, drawing, zoom, selectEdge]);
  const selectedEdge = pipeline.edges.find((edge) => edge.id === selectedEdgeId);

  const fit = useCallback(() => {
    const frame = map.current?.getBoundingClientRect();
    if (!frame?.width || !frame.height) return;
    const current = getNodes();
    const currentStages = pipeline.stages.map((stage) => ({ ...stage, position: current.find((node) => node.id === stage.id)?.position ?? stage.position }));
    void setViewport(dependencyViewport(currentStages, pipeline.edges, frame.width, frame.height), { duration: motion ? 180 : 0 });
  }, [getNodes, pipeline.stages, pipeline.edges, setViewport, motion]);
  const fitRef = useRef(fit);
  useEffect(() => { fitRef.current = fit; }, [fit]);
  useEffect(() => {
    if (!initialized || mode !== 'dag' || !map.current) return;
    const observer = new ResizeObserver(() => {
      if (map.current) setMapWidth(map.current.clientWidth);
      const id = focusAfterView.current;
      const node = id ? getNodes().find((item) => item.id === id) : undefined;
      if (node) {
        focusAfterView.current = null;
        void setCenter(node.position.x + STAGE_WIDTH / 2, node.position.y + (node.measured?.height ?? 120) / 2, { zoom: 1, duration: motion ? 180 : 0 });
      } else fitRef.current();
    });
    observer.observe(map.current);
    return () => observer.disconnect();
  }, [initialized, mode, pipeline.id, getNodes, setCenter, motion]);

  function connect(source: string, target: string) {
    if (!canEdit) return;
    const problem = connectionProblem(pipeline.stages, pipeline.edges, source, target);
    if (problem) { setFeedback(problem); return; }
    connectStages(source, target); setFeedback('Dependency added. Save to keep this change.');
  }
  const onConnect = (connection: Connection) => { if (connection.source && connection.target) connect(connection.source, connection.target); };
  function addAt(type: StageType, position: { x: number; y: number }) {
    if (!canEdit || !stageTypes.includes(type)) return;
    const id = addStage(type, { x: Math.round(position.x - STAGE_WIDTH / 2), y: Math.round(position.y - 60) });
    if (id) selectStage(id);
  }
  function addAtCentre(type: StageType) {
    if (mode === 'list') {
      setMode('dag');
      const last = stages[stages.length - 1];
      addAt(type, { x: (last?.position.x ?? 0) + 300, y: (last?.position.y ?? 120) + 60 });
      return;
    }
    const frame = map.current?.getBoundingClientRect();
    if (!frame) return;
    setMode('dag');
    addAt(type, screenToFlowPosition({ x: frame.left + frame.width / 2, y: frame.top + frame.height / 2 }));
  }
  function drop(event: DragEvent) { event.preventDefault(); addAt(event.dataTransfer.getData(DRAG_MIME) as StageType, screenToFlowPosition({ x: event.clientX, y: event.clientY })); }
  function changeCondition(edge: PipelineEdge, condition: PipelineEdge['condition']) {
    if (!canEdit) return;
    // Preserve the real store's edge ID and existing success/failure/always API.
    for (let i = 0; i < 3; i++) {
      const current = usePipelineStore.getState().pipeline?.edges.find((item) => item.id === edge.id)?.condition ?? 'success';
      if (current === condition) break;
      cycleEdgeCondition(edge.id);
    }
  }
  function focusStage(id: string) {
    const stage = stages.find((item) => item.id === id);
    if (!stage) return;
    if (mode !== 'dag') { focusAfterView.current = id; setMode('dag'); selectStage(id); return; }
    setMode('dag'); selectStage(id);
    void setCenter(stage.position.x + STAGE_WIDTH / 2, stage.position.y + nodeBounds(stage, pipeline.edges).height / 2, { zoom: 1, duration: motion ? 180 : 0 });
  }

  return <div className="pipeline-workspace">
    <section className="pipeline-board" aria-label="Pipeline dependencies">
      <div className="pipeline-toolbar">
        <div role="group" aria-label="Pipeline view"><button type="button" aria-pressed={mode === 'dag'} onClick={() => setMode('dag')}>DAG</button><button type="button" aria-pressed={mode === 'list'} onClick={() => setMode('list')}>Step list</button></div>
        <label className="pipeline-find">Find a step<select value="" onChange={(event) => focusStage(event.target.value)}><option value="">Choose a step…</option>{stages.map((stage) => <option value={stage.id} key={stage.id}>{stage.name}</option>)}</select></label>
        {mode === 'dag' && <div role="group" aria-label="Map zoom"><button type="button" aria-label="Zoom out" onClick={() => void zoomOut()}>−</button><span>{Math.round(zoom * 100)}%</span><button type="button" aria-label="Zoom in" onClick={() => void zoomIn()}>+</button><button type="button" onClick={fit}>Fit map</button></div>}
      </div>
      <div ref={map} className="pipeline-map" hidden={mode !== 'dag'}>
        <ReactFlow nodes={flowNodes} edges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} onNodesChange={onNodesChange}
          onConnect={onConnect} onNodeDragStop={(_, node) => { if (canEdit) moveStage(node.id, { x: Math.round(node.position.x), y: Math.round(node.position.y) }); }}
          onNodesDelete={(deleted: Node[]) => { if (canEdit) deleted.forEach((node) => removeStage(node.id)); }} onEdgesDelete={(deleted: Edge[]) => { if (canEdit) deleted.forEach((edge) => removeEdge(edge.id)); }}
          onNodeClick={(_, node) => selectStage(node.id)} onEdgeClick={(_, edge) => selectEdge(edge.id)} onPaneClick={() => selectStage(null)}
          onDragOver={(event) => { if (canEdit) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; } }} onDrop={drop}
          nodesDraggable={canEdit} nodesConnectable={canEdit} deleteKeyCode={canEdit ? ['Backspace', 'Delete'] : null} minZoom={0.02} maxZoom={2} proOptions={{ hideAttribution: true }} />
        {stages.length === 0 && <p className="pipeline-empty">No steps yet. Add a stage below to begin your pipeline.</p>}
      </div>
      {mode === 'list' && <ol className="pipeline-step-list" aria-label="Pipeline steps">{stages.map((stage) => {
        const incoming = pipeline.edges.filter((edge) => edge.target === stage.id);
        return <li key={stage.id}><button type="button" className="pipeline-list-stage" onClick={() => selectStage(stage.id)} aria-pressed={selectedNodeId === stage.id}><strong>{stage.name}</strong><span>{requirementSummary(incoming.length)}</span></button>
          {incoming.length > 0 && <ul>{incoming.map((edge) => <li key={edge.id}><button type="button" onClick={() => selectEdge(edge.id)}>{pipeline.edges.indexOf(edge)+1}. {dependencyStory(edge, stages).summary}</button></li>)}</ul>}
          <button type="button" className="pipeline-text-action" onClick={() => focusStage(stage.id)}>Show on DAG</button></li>;
      })}</ol>}
      <p className="pipeline-legend"><b>A → B</b> B waits for A. Every incoming requirement must be met. Arrows show execution order, not runtime traffic.</p>
      {canEdit && <StageTray onAdd={addAtCentre} />}
      {feedback && <p className="pipeline-feedback" role="status">{feedback}</p>}
      <details className="pipeline-directory"><summary>All dependencies ({pipeline.edges.length})</summary><ol>{pipeline.edges.map((edge, index) => <li key={edge.id}><button type="button" onClick={() => selectEdge(edge.id)} aria-pressed={selectedEdgeId === edge.id}><span className="pipeline-number">{index+1}</span><span>{dependencyStory(edge, stages).summary}</span></button></li>)}</ol>{!pipeline.edges.length && <p>No steps depend on another yet.</p>}</details>
    </section>
    {selectedEdge ? <aside className="pipeline-inspector" aria-label="Dependency explanation"><div className="pipeline-inspector-heading"><h2>Dependency {pipeline.edges.indexOf(selectedEdge)+1}</h2><button type="button" aria-label="Close dependency" onClick={() => setSelectedEdgeId(null)}>×</button></div>
      <p className="pipeline-rule">{dependencyStory(selectedEdge, stages).summary}</p>
      <dl className="pipeline-dependency-story"><dt>First · prerequisite</dt><dd><button type="button" onClick={() => selectStage(selectedEdge.source)}>{dependencyStory(selectedEdge, stages).source}</button></dd><dt>Then · waiting step</dt><dd><button type="button" onClick={() => selectStage(selectedEdge.target)}>{dependencyStory(selectedEdge, stages).target}</button></dd></dl>
      <div className="pipeline-why"><strong>Why this step waits</strong><p>{dependencyStory(selectedEdge, stages).reason} If other arrows enter the waiting step, their conditions must also be met.</p></div>
      <details className="pipeline-edit-settings"><summary>{canEdit ? 'Edit dependency' : 'Technical details'}</summary><p className="mono">ID: {selectedEdge.id}</p><label htmlFor="pipeline-dependency-condition">Condition</label><select id="pipeline-dependency-condition" disabled={!canEdit} value={selectedEdge.condition ?? 'success'} onChange={(event) => changeCondition(selectedEdge, event.target.value as PipelineEdge['condition'])}><option value="success">After success</option><option value="failure">After failure</option><option value="always">After success, failure, or skip</option></select>{canEdit && <button type="button" className="pipeline-remove" onClick={() => { removeEdge(selectedEdge.id); setSelectedEdgeId(null); }}>Remove dependency</button>}</details>
    </aside> : selectedNodeId ? <StageInspector canEdit={canEdit} onInspectEdge={selectEdge} onConnect={connect} /> : <aside className="pipeline-inspector pipeline-help"><h2>Read the deployment plan</h2><p>Select a numbered arrow to see what waits for what, and why.</p><p>Steps without prerequisites can begin first. Separate branches may run in parallel. A join waits for every incoming requirement.</p><p>Use Step list or Find a step when the map is large.</p>{!canEdit && <p>You have read-only access. An operator or administrator can edit and run pipelines.</p>}</aside>}
  </div>;
}
export default memo(PipelineCanvas);
