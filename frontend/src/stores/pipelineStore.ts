import { create } from 'zustand';
import type { Node, Edge, XYPosition } from '@xyflow/react';
import type { Pipeline, PipelineSourceConfig, Stage, StageType, StageConfig, PipelineEdge } from '../types/pipeline';
import { pipelineApi } from '../api/pipelines';

interface PipelineStore {
  pipeline: Pipeline | null;
  nodes: Node[];
  edges: Edge[];
  selectedNodeId: string | null;
  /** True when the in-memory pipeline differs from what the server last returned. */
  dirty: boolean;

  clearDrafts: () => void;
  discardDraft: (id: string) => Promise<void>;
  loadPipeline: (id: string) => Promise<void>;
  savePipeline: () => Promise<void>;
  setPipeline: (pipeline: Pipeline) => void;
  /** Adds a stage and returns its id (so the canvas can settle it into place). */
  addStage: (type: StageType, position: XYPosition) => string;
  removeStage: (id: string) => void;
  updateStageConfig: (id: string, config: Partial<StageConfig>) => void;
  /** Rename / re-home a stage (name, environment) — config goes through updateStageConfig. */
  updateStage: (id: string, patch: Partial<Pick<Stage, 'name' | 'environmentId'>>) => void;
  /** Persist a canvas drag into the pipeline definition (called on drag stop). */
  moveStage: (id: string, position: XYPosition) => void;
  connectStages: (source: string, target: string, condition?: string) => void;
  removeEdge: (id: string) => void;
  setSelectedNode: (id: string | null) => void;
  setNodes: (nodes: Node[]) => void;
  setEdges: (edges: Edge[]) => void;
  setRunDeadline: (runDeadline: string) => void;
  setSource: (source: PipelineSourceConfig | null) => void;
  cycleEdgeCondition: (id: string) => void;
  validate: () => Promise<{ valid: boolean; errors: string[] }>;
}

let stageCounter = 0;
let loadSequence = 0;
let draftSession = 0;
const pipelineDrafts = new Map<string, Pipeline>();

function stagesToNodes(stages: Stage[]): Node[] {
  return stages.map((stage) => ({
    id: stage.id,
    type: stage.type,
    position: stage.position,
    data: { label: stage.name, config: stage.config, stageType: stage.type, environmentId: stage.environmentId },
  }));
}

function edgesToFlowEdges(edges: PipelineEdge[]): Edge[] {
  // Rendered by components/porthole/ConstellationEdge — colour and dash
  // derive from data.condition there, so no per-edge style is set here.
  return edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: 'constellation',
    data: { condition: edge.condition },
  }));
}

export const usePipelineStore = create<PipelineStore>((set, get) => ({
  pipeline: null,
  nodes: [],
  edges: [],
  selectedNodeId: null,
  dirty: false,

  clearDrafts: () => {
    draftSession++;
    loadSequence++;
    pipelineDrafts.clear();
    set({ pipeline: null, nodes: [], edges: [], selectedNodeId: null, dirty: false });
  },
  discardDraft: async (id: string) => {
    const sequence = ++loadSequence;
    const draft = get().pipeline;
    const saved = await pipelineApi.get(id);
    if (sequence !== loadSequence || get().pipeline !== draft || draft?.id !== id || saved.id !== id) return;
    pipelineDrafts.delete(id);
    set({ pipeline: saved, nodes: stagesToNodes(saved.stages), edges: edgesToFlowEdges(saved.edges), selectedNodeId: null, dirty: false });
  },

  loadPipeline: async (id: string) => {
    const sequence = ++loadSequence;
    const current = get();
    if (current.pipeline && current.dirty) pipelineDrafts.set(current.pipeline.id, current.pipeline);
    const draft = pipelineDrafts.get(id);
    if (draft) {
      set({ pipeline: draft, nodes: stagesToNodes(draft.stages), edges: edgesToFlowEdges(draft.edges), selectedNodeId: null, dirty: true });
      return;
    }
    const pipeline = await pipelineApi.get(id);
    if (sequence !== loadSequence) return;
    set({ pipeline, nodes: stagesToNodes(pipeline.stages), edges: edgesToFlowEdges(pipeline.edges), selectedNodeId: null, dirty: false });
  },

  savePipeline: async () => {
    const { pipeline } = get();
    if (!pipeline) return;
    // The server bumps `version` (optimistic concurrency) — keep its copy so
    // the next save carries the right version. Nodes/edges are left alone:
    // positions already match and regenerating them would churn the canvas.
    const session = draftSession;
    const saved = await pipelineApi.update(pipeline.id, pipeline);
    if (session !== draftSession) return;
    const latest = get().pipeline;
    // Preserve edits made while the request was in flight, and never replace
    // a different pipeline loaded by navigation during that request.
    if (latest?.id !== pipeline.id) {
      const cached = pipelineDrafts.get(pipeline.id);
      if (cached === pipeline) pipelineDrafts.delete(pipeline.id);
      else if (cached) pipelineDrafts.set(pipeline.id, { ...cached, version: saved.version });
      return;
    }
    pipelineDrafts.delete(pipeline.id);
    set(latest === pipeline ? { pipeline: saved, dirty: false } : { pipeline: { ...latest, version: saved.version }, dirty: true });
  },

  setPipeline: (pipeline: Pipeline) => {
    loadSequence++;
    pipelineDrafts.delete(pipeline.id);
    set({
      pipeline,
      nodes: stagesToNodes(pipeline.stages),
      edges: edgesToFlowEdges(pipeline.edges),
      dirty: false,
    });
  },

  addStage: (type: StageType, position: XYPosition) => {
    const { pipeline } = get();
    if (!pipeline) return '';

    const id = `stage-${++stageCounter}-${Date.now()}`;
    const newStage: Stage = {
      id,
      name: `${type.charAt(0).toUpperCase() + type.slice(1)} Stage`,
      type,
      config: {},
      position,
    };

    const updatedPipeline = {
      ...pipeline,
      stages: [...pipeline.stages, newStage],
    };

    set({
      pipeline: updatedPipeline,
      nodes: stagesToNodes(updatedPipeline.stages),
      dirty: true,
    });
    return id;
  },

  removeStage: (id: string) => {
    const { pipeline } = get();
    if (!pipeline) return;

    const updatedPipeline = {
      ...pipeline,
      stages: pipeline.stages.filter((s) => s.id !== id),
      edges: pipeline.edges.filter((e) => e.source !== id && e.target !== id),
    };

    set({
      pipeline: updatedPipeline,
      nodes: stagesToNodes(updatedPipeline.stages),
      edges: edgesToFlowEdges(updatedPipeline.edges),
      selectedNodeId: null,
      dirty: true,
    });
  },

  updateStageConfig: (id: string, config: Partial<StageConfig>) => {
    const { pipeline } = get();
    if (!pipeline) return;

    const updatedPipeline = {
      ...pipeline,
      stages: pipeline.stages.map((s) =>
        s.id === id ? { ...s, config: { ...s.config, ...config } } : s
      ),
    };

    set({
      pipeline: updatedPipeline,
      nodes: stagesToNodes(updatedPipeline.stages),
      dirty: true,
    });
  },

  updateStage: (id: string, patch: Partial<Pick<Stage, 'name' | 'environmentId'>>) => {
    const { pipeline } = get();
    if (!pipeline) return;
    const updatedPipeline = {
      ...pipeline,
      stages: pipeline.stages.map((s) => (s.id === id ? { ...s, ...patch } : s)),
    };
    set({
      pipeline: updatedPipeline,
      nodes: stagesToNodes(updatedPipeline.stages),
      dirty: true,
    });
  },

  moveStage: (id: string, position: XYPosition) => {
    const { pipeline } = get();
    if (!pipeline) return;
    const stage = pipeline.stages.find((s) => s.id === id);
    if (!stage || (stage.position.x === position.x && stage.position.y === position.y)) return;
    // Definition only — the canvas already holds the dragged node position.
    set({
      pipeline: {
        ...pipeline,
        stages: pipeline.stages.map((s) => (s.id === id ? { ...s, position } : s)),
      },
      dirty: true,
    });
  },

  connectStages: (source: string, target: string, condition?: string) => {
    const { pipeline } = get();
    if (!pipeline) return;

    if (pipeline.edges.some((e) => e.source === source && e.target === target)) return;
    const newEdge: PipelineEdge = {
      id: `edge-${source}-${target}`,
      source,
      target,
      condition: condition as PipelineEdge['condition'],
    };

    const updatedPipeline = {
      ...pipeline,
      edges: [...pipeline.edges, newEdge],
    };

    set({
      pipeline: updatedPipeline,
      edges: edgesToFlowEdges(updatedPipeline.edges),
      dirty: true,
    });
  },

  removeEdge: (id: string) => {
    const { pipeline } = get();
    if (!pipeline) return;

    const updatedPipeline = {
      ...pipeline,
      edges: pipeline.edges.filter((e) => e.id !== id),
    };

    set({
      pipeline: updatedPipeline,
      edges: edgesToFlowEdges(updatedPipeline.edges),
      dirty: true,
    });
  },

  setSelectedNode: (id: string | null) => set({ selectedNodeId: id }),
  setNodes: (nodes: Node[]) => set({ nodes }),
  setEdges: (edges: Edge[]) => set({ edges }),

  setRunDeadline: (runDeadline: string) => {
    const { pipeline } = get();
    if (!pipeline) return;
    set({ pipeline: { ...pipeline, runDeadline: runDeadline || undefined }, dirty: true });
  },

  setSource: (source: PipelineSourceConfig | null) => {
    const { pipeline } = get();
    if (!pipeline) return;
    // A null explicitly removes a binding on PUT. Canvas positions stay intact.
    set({ pipeline: { ...pipeline, source: source ? { ...source } : null }, dirty: true });
  },

  cycleEdgeCondition: (id: string) => {
    const { pipeline } = get();
    if (!pipeline) return;
    // (none ≡ success) → failure → always → (none). The canvas refreshes
    // its local edge state from the store after calling this.
    const nextOf = (c?: PipelineEdge['condition']): PipelineEdge['condition'] =>
      c === 'failure' ? 'always' : c === 'always' ? undefined : 'failure';
    const updatedPipeline = {
      ...pipeline,
      edges: pipeline.edges.map((e) => (e.id === id ? { ...e, condition: nextOf(e.condition) } : e)),
    };
    set({
      pipeline: updatedPipeline,
      edges: edgesToFlowEdges(updatedPipeline.edges),
      dirty: true,
    });
  },

  validate: async () => {
    const { pipeline } = get();
    if (!pipeline) return { valid: false, errors: ['No pipeline loaded'] };
    return pipelineApi.validate(pipeline.id);
  },
}));
