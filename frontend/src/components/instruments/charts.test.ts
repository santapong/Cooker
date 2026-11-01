import { describe, expect, it } from 'vitest';
import { linePath, niceTicks, scaleLinear, tickDuration } from './charts';

describe('chart geometry', () => {
  it('nice ticks cover the max with round steps', () => {
    expect(niceTicks(0)).toEqual([0]);
    expect(niceTicks(95)).toEqual([0, 20, 40, 60, 80, 100]);
    expect(niceTicks(7)).toEqual([0, 2, 4, 6, 8]);
    expect(niceTicks(360_000)).toEqual([0, 100000, 200000, 300000, 400000]);
  });
  it('scales linearly and guards an empty domain', () => {
    expect(scaleLinear(100, 200)(50)).toBe(100);
    expect(scaleLinear(0, 200)(50)).toBe(0);
  });
  it('builds a polyline path', () => {
    expect(linePath([])).toBe('');
    expect(linePath([{ x: 0, y: 10 }, { x: 20.04, y: 5 }])).toBe('M 0 10 L 20 5');
  });
  it('formats durations for ticks', () => {
    expect(tickDuration(250)).toBe('250ms');
    expect(tickDuration(1200)).toBe('1.2s');
    expect(tickDuration(45_000)).toBe('45s');
    expect(tickDuration(180_000)).toBe('3m');
    expect(tickDuration(3_840_000)).toBe('1h 04m');
  });
});
