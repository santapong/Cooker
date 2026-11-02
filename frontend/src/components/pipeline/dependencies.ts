import type { PipelineEdge, Stage } from '../../types/pipeline';
import { getViewportForBounds } from '@xyflow/react';

export const STAGE_WIDTH = 204;
export const stageHeight = (ports: number) => Math.max(120, 56 + ports * 18);
export type Point = { x: number; y: number };
export type Bounds = Point & { width: number; height: number };
export interface DependencyDrawing {
  id: string; number: number; path: string; source: Point; target: Point;
  label: Point; anchor: Point; bounds: Bounds;
}

export function dependencyStory(edge: PipelineEdge, stages: Stage[]) {
  const source = stages.find((stage) => stage.id === edge.source)?.name ?? `Missing step (${edge.source})`;
  const target = stages.find((stage) => stage.id === edge.target)?.name ?? `Missing step (${edge.target})`;
  const condition = edge.condition ?? 'success';
  const rule = condition === 'failure' ? 'fails' : condition === 'always' ? 'finishes or is skipped' : 'succeeds';
  const reason = condition === 'failure' ? 'This is a failure-handling path. It is eligible when the prerequisite fails.'
    : condition === 'always' ? 'This requirement accepts a prerequisite that succeeded, failed, or was skipped.'
      : 'This step needs the prerequisite to complete successfully before it is eligible to run.';
  return { source, target, condition, summary: `${target} waits until ${source} ${rule}.`, reason };
}

export function requirementSummary(count: number): string {
  return count === 0 ? 'No prerequisites · can start first' : count === 1 ? 'Waits for 1 requirement' : `Waits for all ${count} requirements`;
}

/** A proposed connection is checked before touching the real pipeline store. */
export function connectionProblem(stages: Stage[], edges: PipelineEdge[], source: string, target: string): string | null {
  if (!stages.some((stage) => stage.id === source) || !stages.some((stage) => stage.id === target)) return 'Both steps must exist.';
  if (source === target) return 'A step cannot wait for itself.';
  if (edges.some((edge) => edge.source === source && edge.target === target)) return 'These steps already have a dependency.';
  const pending = [target]; const seen = new Set<string>();
  while (pending.length) {
    const id = pending.pop()!;
    if (id === source) return 'This connection would create a cycle. A prerequisite cannot depend on the step that waits for it.';
    if (seen.has(id)) continue;
    seen.add(id);
    pending.push(...edges.filter((edge) => edge.source === id).map((edge) => edge.target));
  }
  return null;
}

export function nodeBounds(stage: Stage, edges: PipelineEdge[]): Bounds {
  return { ...stage.position, width: STAGE_WIDTH, height: stageHeight(Math.max(edges.filter((edge) => edge.source === stage.id).length, edges.filter((edge) => edge.target === stage.id).length)) };
}
export function portTop(index: number, count: number, height: number): number {
  return 40 + (index + 1) * (height - 52) / (count + 1);
}
const overlaps = (a: Bounds, b: Bounds) => a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y;
const boxAt = (point: Point, radius: number): Bounds => ({ x: point.x - radius, y: point.y - radius, width: radius * 2, height: radius * 2 });

/** Shared geometry makes labels, arrows and Fit use the same real node positions. */
export function dependencyDrawings(stages: Stage[], edges: PipelineEdge[], zoom = 1, labelWidth = Infinity): DependencyDrawing[] {
  const nodes = new Map(stages.map((stage) => [stage.id, nodeBounds(stage, edges)]));
  const occupied: Bounds[] = [];
  const radius = 24 / Math.max(zoom, 0.02);
  const boxes = [...nodes.values()];
  const center = { x: boxes.length ? (Math.min(...boxes.map((box) => box.x)) + Math.max(...boxes.map((box) => box.x + box.width))) / 2 : 0, y: boxes.length ? (Math.min(...boxes.map((box) => box.y)) + Math.max(...boxes.map((box) => box.y + box.height))) / 2 : 0 };
  return edges.flatMap((edge, index) => {
    const from = nodes.get(edge.source); const to = nodes.get(edge.target);
    if (!from || !to) return [];
    const outgoing = edges.filter((item) => item.source === edge.source);
    const incoming = edges.filter((item) => item.target === edge.target);
    const source = { x: from.x + from.width, y: from.y + portTop(outgoing.findIndex((item) => item.id === edge.id), outgoing.length, from.height) };
    const target = { x: to.x, y: to.y + portTop(incoming.findIndex((item) => item.id === edge.id), incoming.length, to.height) };
    const lead = Math.max(48, Math.abs(target.x - source.x) / 2);
    const c1 = { x: source.x + lead, y: source.y }; const c2 = { x: target.x - lead, y: target.y };
    const pointAt = (t: number): Point => ({ x: (1-t)**3*source.x + 3*(1-t)**2*t*c1.x + 3*(1-t)*t*t*c2.x + t**3*target.x, y: (1-t)**3*source.y + 3*(1-t)**2*t*c1.y + 3*(1-t)*t*t*c2.y + t**3*target.y });
    const candidates = [0.5, 0.35, 0.65, 0.2, 0.8, 0.425, 0.575, 0.275, 0.725].map(pointAt);
    const blocked = (point: Point) => Math.abs(point.x - center.x) + radius > labelWidth / (2 * zoom) || [...nodes.values(), ...occupied].some((box) => overlaps(boxAt(point, radius), box));
    let anchor = candidates.find((point) => !blocked(point)) ?? candidates[0];
    let label = anchor;
    // Crowded joins retain individually selectable numbers. A short leader
    // connects any displaced label back to its own path, never to another edge.
    if (blocked(label)) {
      let placed = false;
      // At small viewports use a bounded column grid for displaced labels.
      // Fixed screen-size targets cannot be fitted by zooming them smaller.
      if (Number.isFinite(labelWidth)) {
        const columns = Math.max(1, Math.floor(labelWidth / 49.2));
        for (let row = 0; row <= edges.length + 4 && !placed; row++) {
          for (const direction of row === 0 ? [0] : [-1, 1]) for (let column = 0; column < columns && !placed; column++) {
            const candidate = { x: center.x + (column - (columns - 1) / 2) * radius * 2.05, y: center.y + row * direction * radius * 2.05 };
            if (!blocked(candidate)) { label = candidate; placed = true; }
          }
        }
      }
      for (let ring = 1; ring <= edges.length + 4 && !placed; ring++) {
        for (const [dx, dy] of [[0, -1], [0, 1], [-1, 0], [1, 0], [-1, -1], [1, -1], [-1, 1], [1, 1]]) {
          const candidate = { x: anchor.x + dx * ring * radius * 2.2, y: anchor.y + dy * ring * radius * 2.2 };
          if (!blocked(candidate)) { label = candidate; placed = true; break; }
        }
      }
    }
    occupied.push(boxAt(label, radius));
    anchor = { ...anchor };
    const xs = [source.x, target.x, c1.x, c2.x, label.x-radius, label.x+radius];
    const ys = [source.y, target.y, c1.y, c2.y, label.y-radius, label.y+radius];
    return [{ id: edge.id, number: index + 1, source, target, label, anchor, path: `M ${source.x} ${source.y} C ${c1.x} ${c1.y}, ${c2.x} ${c2.y}, ${target.x} ${target.y}`, bounds: { x: Math.min(...xs), y: Math.min(...ys), width: Math.max(...xs)-Math.min(...xs), height: Math.max(...ys)-Math.min(...ys) } }];
  });
}

export function graphBounds(stages: Stage[], edges: PipelineEdge[], zoom = 1, labelWidth = Infinity): Bounds {
  const boxes = [...stages.map((stage) => nodeBounds(stage, edges)), ...dependencyDrawings(stages, edges, zoom, labelWidth).map((edge) => edge.bounds)];
  if (!boxes.length) return { x: 0, y: 0, width: 400, height: 240 };
  const x = Math.min(...boxes.map((box) => box.x)); const y = Math.min(...boxes.map((box) => box.y));
  return { x, y, width: Math.max(...boxes.map((box) => box.x + box.width))-x, height: Math.max(...boxes.map((box) => box.y + box.height))-y };
}

/** Labels stay 44 screen pixels, so fit iterates against their actual zoom. */
export function dependencyViewport(stages: Stage[], edges: PipelineEdge[], width: number, height: number) {
  let zoom = 1;
  for (let i = 0; i < 30; i++) {
    const next = getViewportForBounds(graphBounds(stages, edges, zoom, width * 0.78), width, height, 0.02, 1, 0.18).zoom;
    if (Math.abs(next - zoom) < 0.0001) { zoom = next; break; }
    zoom = next;
  }
  return getViewportForBounds(graphBounds(stages, edges, zoom, width * 0.78), width, height, 0.02, 1, 0.18);
}
