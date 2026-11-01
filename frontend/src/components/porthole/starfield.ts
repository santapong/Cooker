/**
 * Deterministic starfield generator — pure, seedable, unit-testable.
 * Positions are percentages of the field; sizes in px; a sparse subset
 * twinkles (period ≥ 7 s, phase-shifted by `delay`) per spec §3 / SC 2.3.1.
 */
export interface Star {
  /** 0–100, percent of field width. */
  x: number;
  /** 0–100, percent of field height. */
  y: number;
  /** Diameter in px at 1× scale. */
  size: number;
  /** Resting opacity 0.15–0.65. */
  opacity: number;
  twinkle: boolean;
  /** Twinkle phase offset in seconds, 0–TWINKLE_PERIOD_S. */
  delay: number;
}

/** Twinkle period in seconds. Must stay ≥ 7 (flash-safety floor with margin). */
export const TWINKLE_PERIOD_S = 9;
/** Share of stars that twinkle. Sparse on purpose. */
export const TWINKLE_SHARE = 0.18;

/** mulberry32 — tiny seedable PRNG, good enough for decoration. */
export function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export function makeStars(seed: number, count: number, sizeMin: number, sizeMax: number): Star[] {
  const rand = mulberry32(seed);
  const stars: Star[] = [];
  for (let i = 0; i < count; i++) {
    const x = rand() * 100;
    const y = rand() * 100;
    const size = sizeMin + rand() * (sizeMax - sizeMin);
    const opacity = 0.15 + rand() * 0.5;
    const twinkle = rand() < TWINKLE_SHARE;
    const delay = twinkle ? rand() * TWINKLE_PERIOD_S : 0;
    stars.push({
      x: round(x, 2),
      y: round(y, 2),
      size: round(size, 2),
      opacity: round(opacity, 2),
      twinkle,
      delay: round(delay, 1),
    });
  }
  return stars;
}

function round(v: number, places: number): number {
  const f = 10 ** places;
  return Math.round(v * f) / f;
}
