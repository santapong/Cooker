import type { Stage, PipelineEdge } from '../types/pipeline';

export function validateDAG(stages: Stage[], edges: PipelineEdge[]): string[] {
  const errors: string[] = [];
  const stageIds = new Set(stages.map((s) => s.id));

  // Check for duplicate IDs
  if (stageIds.size !== stages.length) {
    errors.push('Duplicate stage IDs detected');
  }

  // Check edge references
  for (const edge of edges) {
    if (!stageIds.has(edge.source)) errors.push(`Edge references unknown source: ${edge.source}`);
    if (!stageIds.has(edge.target)) errors.push(`Edge references unknown target: ${edge.target}`);
  }

  // Cycle detection (Kahn's algorithm)
  const inDegree = new Map<string, number>();
  const adj = new Map<string, string[]>();

  for (const id of stageIds) {
    inDegree.set(id, 0);
    adj.set(id, []);
  }

  for (const edge of edges) {
    adj.get(edge.source)?.push(edge.target);
    inDegree.set(edge.target, (inDegree.get(edge.target) || 0) + 1);
  }

  const queue: string[] = [];
  for (const [id, deg] of inDegree) {
    if (deg === 0) queue.push(id);
  }

  let visited = 0;
  while (queue.length > 0) {
    const node = queue.shift()!;
    visited++;
    for (const next of adj.get(node) || []) {
      const newDeg = (inDegree.get(next) || 1) - 1;
      inDegree.set(next, newDeg);
      if (newDeg === 0) queue.push(next);
    }
  }

  if (visited !== stageIds.size) {
    errors.push('Pipeline contains a cycle');
  }

  return errors;
}
