import { describe, expect, it } from 'vitest';
import type { Stage } from '../../types/pipeline';
import { layoutMini } from './miniConstellation';

const st = (id: string, x: number, y: number): Stage => ({ id, name: id, type: 'custom', config: {}, position: { x, y } });

describe('layoutMini', () => {
  it('is empty for no stages', () => {
    expect(layoutMini([], [], 72, 40)).toEqual({ points: [], segments: [] });
  });
  it('centres a single star', () => {
    expect(layoutMini([st('a', 500, 900)], [], 72, 40).points).toEqual([{ id: 'a', x: 36, y: 20 }]);
  });
  it('fits the bounding box inside the padding and keeps edges between known stars', () => {
    const stages = [st('a', 0, 0), st('b', 1000, 300), st('c', 500, 600)];
    const { points, segments } = layoutMini(stages, [
      { id: 'ab', source: 'a', target: 'b' },
      { id: 'zz', source: 'z', target: 'b' },
    ], 72, 40);
    expect(points[0]).toEqual({ id: 'a', x: 6, y: 6 });
    expect(points[1]).toEqual({ id: 'b', x: 66, y: 20 });
    expect(points[2]).toEqual({ id: 'c', x: 36, y: 34 });
    expect(segments).toEqual([{ id: 'ab', x1: 6, y1: 6, x2: 66, y2: 20 }]);
  });
  it('centres a degenerate axis', () => {
    const { points } = layoutMini([st('a', 0, 100), st('b', 300, 100)], [], 72, 40);
    expect(points.map((p) => p.y)).toEqual([20, 20]);
  });
});
