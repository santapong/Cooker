import { create } from 'zustand';
import type { Node, Edge, XYPosition } from '@xyflow/react';
import type { Pipeline, Stage, StageType, StageConfig, PipelineEdge } from '../types/pipeline';
import { pipelineApi } from '../api/pipelines';

interface PipelineStore {
  pipeline: Pipeline | null;
  nodes: Node[];
  edges: Edge[];
  selectedNodeId: string | null;

  loadPipeline: (id: string) => Promise<void>;
  savePipeline: () => Promise<void>;
  setPipeline: (pipeline: Pipeline) => void;
  addStage: (type: StageType, position: XYPosition) => void;
  removeStage: (id: string) => void;
  updateStageConfig: (id: string, config: Partial<StageConfig>) => void;
  connectStages: (source: string, target: string, condition?: string) => void;
  removeEdge: (id: string) => void;
  setSelectedNode: (id: string | null) => void;
  setNodes: (nodes: Node[]) => void;
  setEdges: (edges: Edge[]) => void;
  validate: () => Promise<{ valid: boolean; errors: string[] }>;
}

let stageCounter = 0;

function stagesToNodes(stages: Stage[]): Node[] {
  return stages.map((stage) => ({
    id: stage.id,
    type: stage.type,
    position: stage.position,
    data: { label: stage.name, config: stage.config, stageType: stage.type, environmentId: stage.environmentId },
  }));
}

function edgesToFlowEdges(edges: PipelineEdge[]): Edge[] {
  return edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    label: edge.condition || '',
    animated: false,
    style: { stroke: edge.condition === 'failure' ? '#ef4444' : '#3b82f6' },
  }));
}

export const usePipelineStore = create<PipelineStore>((set, get) => ({
  pipeline: null,
  nodes: [],
  edges: [],
  selectedNodeId: null,

  loadPipeline: async (id: string) => {
    const pipeline = await pipelineApi.get(id);
    set({
      pipeline,
      nodes: stagesToNodes(pipeline.stages),
      edges: edgesToFlowEdges(pipeline.edges),
    });
  },

  savePipeline: async () => {
    const { pipeline } = get();
    if (!pipeline) return;
    await pipelineApi.update(pipeline.id, pipeline);
  },

  setPipeline: (pipeline: Pipeline) => {
    set({
      pipeline,
      nodes: stagesToNodes(pipeline.stages),
      edges: edgesToFlowEdges(pipeline.edges),
    });
  },

  addStage: (type: StageType, position: XYPosition) => {
    const { pipeline } = get();
    if (!pipeline) return;

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
    });
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
    });
  },

  connectStages: (source: string, target: string, condition?: string) => {
    const { pipeline } = get();
    if (!pipeline) return;

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
    });
  },

  setSelectedNode: (id: string | null) => set({ selectedNodeId: id }),
  setNodes: (nodes: Node[]) => set({ nodes }),
  setEdges: (edges: Edge[]) => set({ edges }),

  validate: async () => {
    const { pipeline } = get();
    if (!pipeline) return { valid: false, errors: ['No pipeline loaded'] };
    return pipelineApi.validate(pipeline.id);
  },
}));
