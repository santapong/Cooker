import type { PipelineEdge, Stage } from '../../types/pipeline';

export interface MiniPoint {
  id: string;
  x: number;
  y: number;
}
export interface MiniSegment {
  id: string;
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}
export interface MiniLayout {
  points: MiniPoint[];
  segments: MiniSegment[];
}

/**
 * Fit a pipeline's real stage positions into a small box, preserving the
 * constellation's shape. A single star sits at the centre; a degenerate
 * axis (all stars on one line) is centred on that axis.
 */
export function layoutMini(stages: Stage[], edges: PipelineEdge[], width: number, height: number, pad = 6): MiniLayout {
  if (stages.length === 0) return { points: [], segments: [] };
  const xs = stages.map((s) => s.position.x);
  const ys = stages.map((s) => s.position.y);
  const minX = Math.min(...xs);
  const maxX = Math.max(...xs);
  const minY = Math.min(...ys);
  const maxY = Math.max(...ys);
  const spanX = maxX - minX;
  const spanY = maxY - minY;
  const innerW = width - 2 * pad;
  const innerH = height - 2 * pad;
  const fx = (x: number) => (spanX === 0 ? width / 2 : pad + ((x - minX) / spanX) * innerW);
  const fy = (y: number) => (spanY === 0 ? height / 2 : pad + ((y - minY) / spanY) * innerH);
  const points = stages.map((s) => ({ id: s.id, x: r(fx(s.position.x)), y: r(fy(s.position.y)) }));
  const byId = new Map(points.map((p) => [p.id, p]));
  const segments: MiniSegment[] = [];
  for (const e of edges) {
    const a = byId.get(e.source);
    const b = byId.get(e.target);
    if (a && b) segments.push({ id: e.id, x1: a.x, y1: a.y, x2: b.x, y2: b.y });
  }
  return { points, segments };
}

function r(n: number): number {
  return Math.round(n * 10) / 10;
}
