import { describe, expect, it } from 'vitest';
import { makeStars, mulberry32, TWINKLE_PERIOD_S, TWINKLE_SHARE } from './starfield';

describe('starfield generator', () => {
  it('is deterministic for a seed', () => {
    expect(makeStars(42, 30, 1, 2)).toEqual(makeStars(42, 30, 1, 2));
    expect(makeStars(42, 30, 1, 2)).not.toEqual(makeStars(43, 30, 1, 2));
  });

  it('keeps every star inside the field and the size range', () => {
    for (const s of makeStars(7, 200, 1, 1.6)) {
      expect(s.x).toBeGreaterThanOrEqual(0);
      expect(s.x).toBeLessThanOrEqual(100);
      expect(s.y).toBeGreaterThanOrEqual(0);
      expect(s.y).toBeLessThanOrEqual(100);
      expect(s.size).toBeGreaterThanOrEqual(1);
      expect(s.size).toBeLessThanOrEqual(1.6);
      expect(s.opacity).toBeGreaterThanOrEqual(0.15);
      expect(s.opacity).toBeLessThanOrEqual(0.65);
    }
  });

  it('twinkles sparsely, with phase offsets inside one ≥7 s period', () => {
    expect(TWINKLE_PERIOD_S).toBeGreaterThanOrEqual(7);
    const stars = makeStars(11, 1000, 1, 2);
    const share = stars.filter((s) => s.twinkle).length / stars.length;
    expect(share).toBeGreaterThan(TWINKLE_SHARE - 0.06);
    expect(share).toBeLessThan(TWINKLE_SHARE + 0.06);
    for (const s of stars) {
      if (s.twinkle) {
        expect(s.delay).toBeGreaterThanOrEqual(0);
        expect(s.delay).toBeLessThanOrEqual(TWINKLE_PERIOD_S);
      } else {
        expect(s.delay).toBe(0);
      }
    }
  });

  it('prng yields values in [0, 1)', () => {
    const r = mulberry32(1);
    for (let i = 0; i < 1000; i++) {
      const v = r();
      expect(v).toBeGreaterThanOrEqual(0);
      expect(v).toBeLessThan(1);
    }
  });
});
