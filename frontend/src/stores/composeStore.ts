import { create } from 'zustand';
import type { Node, Edge } from '@xyflow/react';
import type { ComposeGraph, ComposeService } from '../types/compose';
import { dockerApi } from '../api/docker';

interface ComposeStore {
  graph: ComposeGraph | null;
  nodes: Node[];
  edges: Edge[];
  selectedServiceName: string | null;
  loading: boolean;
  error: string | null;

  fetchComposeGraph: (composePath?: string) => Promise<void>;
  setSelectedService: (name: string | null) => void;
  updateServiceConfig: (name: string, config: Partial<ComposeService>) => Promise<void>;
}

const connectionColors: Record<string, string> = {
  depends_on: '#3b82f6',
  env_reference: '#f97316',
  network: '#22c55e',
};

function layoutNodes(services: ComposeGraph['services'], connections: ComposeGraph['connections']): Node[] {
  // Build adjacency for topological sort
  const deps = new Map<string, string[]>();
  const inDegree = new Map<string, number>();
  for (const svc of services) {
    deps.set(svc.name, []);
    inDegree.set(svc.name, 0);
  }
  for (const conn of connections) {
    if (conn.type === 'depends_on') {
      deps.get(conn.target)?.push(conn.source);
      inDegree.set(conn.source, (inDegree.get(conn.source) || 0) + 1);
    }
  }

  // Topological sort by levels (BFS)
  const levels: string[][] = [];
  const queue = services.filter((s) => (inDegree.get(s.name) || 0) === 0).map((s) => s.name);
  const visited = new Set<string>();

  while (queue.length > 0) {
    const level = [...queue];
    levels.push(level);
    const next: string[] = [];
    for (const name of level) {
      visited.add(name);
      for (const dep of deps.get(name) || []) {
        const newDeg = (inDegree.get(dep) || 1) - 1;
        inDegree.set(dep, newDeg);
        if (newDeg === 0 && !visited.has(dep)) {
          next.push(dep);
        }
      }
    }
    queue.length = 0;
    queue.push(...next);
  }

  // Place any remaining unvisited nodes
  for (const svc of services) {
    if (!visited.has(svc.name)) {
      levels.push([svc.name]);
    }
  }

  const nodeWidth = 220;
  const nodeHeight = 120;
  const xGap = 80;
  const yGap = 60;

  const serviceMap = new Map(services.map((s) => [s.name, s]));
  const nodes: Node[] = [];

  for (let col = 0; col < levels.length; col++) {
    const level = levels[col];
    const totalHeight = level.length * nodeHeight + (level.length - 1) * yGap;
    const startY = -totalHeight / 2;

    for (let row = 0; row < level.length; row++) {
      const name = level[row];
      const svc = serviceMap.get(name);
      if (!svc) continue;

      nodes.push({
        id: name,
        type: 'dockerService',
        position: {
          x: col * (nodeWidth + xGap),
          y: startY + row * (nodeHeight + yGap),
        },
        data: {
          label: name,
          service: svc,
        },
      });
    }
  }

  return nodes;
}

function buildEdges(connections: ComposeGraph['connections']): Edge[] {
  return connections.map((conn, i) => ({
    id: `${conn.source}-${conn.target}-${conn.type}-${i}`,
    source: conn.source,
    target: conn.target,
    type: 'serviceConnection',
    label: conn.label,
    style: { stroke: connectionColors[conn.type] || '#475569' },
    data: { connectionType: conn.type },
  }));
}

export const useComposeStore = create<ComposeStore>((set, get) => ({
  graph: null,
  nodes: [],
  edges: [],
  selectedServiceName: null,
  loading: false,
  error: null,

  fetchComposeGraph: async (composePath?: string) => {
    set({ loading: true, error: null });
    try {
      const graph = await dockerApi.parseCompose(composePath);
      const nodes = layoutNodes(graph.services, graph.connections);
      const edges = buildEdges(graph.connections);
      set({ graph, nodes, edges, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  setSelectedService: (name) => {
    set({ selectedServiceName: name });
  },

  updateServiceConfig: async (name, config) => {
    try {
      await dockerApi.updateComposeService(name, config);
      await get().fetchComposeGraph();
    } catch (e) {
      set({ error: (e as Error).message });
    }
  },
}));
