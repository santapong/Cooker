import type { StageConfig, StageType } from '../../types/pipeline';

/** Scene budget for the constellation draw-in (spec §3): everything settled by 600 ms. */
export const SCENE_BUDGET_MS = 600;
/** Lead before the first star — lets the porthole frame land first. */
export const DRAW_BASE_MS = 60;
/** Total span across which star starts are spread. */
export const STAR_SPAN_MS = 240;
/** Per-star pop duration. */
export const STAR_IN_MS = 280;
/** Edges follow their stars: first edge start, per-edge step, last start cap. */
export const EDGE_BASE_MS = 120;
export const EDGE_STEP_MS = 30;
export const EDGE_LAST_START_MS = SCENE_BUDGET_MS - STAR_IN_MS;

/** Per-star stagger: `min(30 ms, 240 / N)` so 20+ stars still settle inside the budget. */
export function starStagger(count: number): number {
  return Math.min(30, STAR_SPAN_MS / Math.max(count, 1));
}

/** Absolute animation delay for star `index` of `count`. */
export function drawDelay(index: number, count: number): number {
  return Math.round(DRAW_BASE_MS + index * starStagger(count));
}

/** Absolute animation delay for edge `index` (capped so the last edge still settles by 600 ms). */
export function edgeDelay(index: number): number {
  return Math.min(EDGE_BASE_MS + index * EDGE_STEP_MS, EDGE_LAST_START_MS);
}

/**
 * Constellation line: a quadratic curve whose control point sits `lift` px
 * above the midpoint, so long edges arc gently instead of ruling straight.
 */
export function edgePath(sx: number, sy: number, tx: number, ty: number, lift = 18): string {
  const mx = (sx + tx) / 2;
  const my = (sy + ty) / 2 - lift;
  return `M ${r(sx)} ${r(sy)} Q ${r(mx)} ${r(my)} ${r(tx)} ${r(ty)}`;
}

/** Point on the curve at t = 0.5 — where a condition label sits. */
export function edgeMidpoint(sx: number, sy: number, tx: number, ty: number, lift = 18): { x: number; y: number } {
  const cx = (sx + tx) / 2;
  const cy = (sy + ty) / 2 - lift;
  // Quadratic Bézier at t = .5: 0.25·P0 + 0.5·C + 0.25·P1
  return { x: 0.25 * sx + 0.5 * cx + 0.25 * tx, y: 0.25 * sy + 0.5 * cy + 0.25 * ty };
}

function r(n: number): number {
  return Math.round(n * 10) / 10;
}

/** Short mono sub-label under a star — the one fact that identifies the stage. */
export function stageSub(type: StageType, config: StageConfig): string {
  switch (type) {
    case 'build':
      return config.tags?.[0]?.split('/').pop() ?? config.dockerfile ?? 'image';
    case 'test':
      return config.image?.split('/').pop()?.split(':')[0] ?? 'test';
    case 'push':
      return config.registry ?? 'registry';
    case 'deploy':
      return config.namespace ?? config.deployRuntime ?? 'deploy';
    case 'approval':
      return 'gate';
    case 'custom':
      return config.image?.split('/').pop()?.split(':')[0] ?? 'script';
    default:
      return type;
  }
}
