import { describe, expect, it } from 'vitest';
import { drawDelay, edgeDelay, edgeMidpoint, edgePath, SCENE_BUDGET_MS, STAR_IN_MS, stageSub, starStagger } from './constellation';

describe('draw-in choreography', () => {
  it('staggers 30 ms for small scenes and compresses for large ones', () => {
    expect(starStagger(5)).toBe(30);
    expect(starStagger(8)).toBe(30);
    expect(starStagger(20)).toBe(12);
    expect(starStagger(120)).toBe(2);
  });

  it('every star and edge has settled inside the 600 ms scene budget', () => {
    for (const n of [1, 6, 20, 60, 200]) {
      const lastStart = drawDelay(n - 1, n);
      expect(lastStart + STAR_IN_MS).toBeLessThanOrEqual(SCENE_BUDGET_MS);
    }
    for (let i = 0; i < 100; i++) expect(edgeDelay(i) + STAR_IN_MS).toBeLessThanOrEqual(SCENE_BUDGET_MS);
    // edges never start before the first star
    expect(edgeDelay(0)).toBeGreaterThan(drawDelay(0, 6));
  });
});

describe('edge geometry', () => {
  it('builds a quadratic path lifted above the midpoint', () => {
    expect(edgePath(0, 100, 200, 100)).toBe('M 0 100 Q 100 82 200 100');
  });

  it('midpoint is the curve at t = 0.5', () => {
    expect(edgeMidpoint(0, 100, 200, 100)).toEqual({ x: 100, y: 91 });
  });
});

describe('stageSub', () => {
  it('picks the identifying fact per stage type', () => {
    expect(stageSub('build', { tags: ['ghcr.io/acme/web:latest'] })).toBe('web:latest');
    expect(stageSub('build', {})).toBe('image');
    expect(stageSub('test', { image: 'grafana/k6:0.50' })).toBe('k6');
    expect(stageSub('push', { registry: 'ghcr.io' })).toBe('ghcr.io');
    expect(stageSub('deploy', { namespace: 'web' })).toBe('web');
    expect(stageSub('approval', {})).toBe('gate');
    expect(stageSub('custom', {})).toBe('script');
  });
});
