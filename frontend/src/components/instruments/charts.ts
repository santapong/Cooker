/** Pure chart geometry for the analytics page — testable, no DOM. */

export interface Pt {
  x: number;
  y: number;
}

export interface LinePoint<T> {
  x: number;
  y: number;
  datum: T;
}

/** Nice axis ticks: 4–5 round steps covering [0, max]. */
export function niceTicks(max: number, count = 4): number[] {
  if (!Number.isFinite(max) || max <= 0) return [0];
  const raw = max / count;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const norm = raw / mag;
  const step = (norm < 1.5 ? 1 : norm < 3 ? 2 : norm < 7 ? 5 : 10) * mag;
  const ticks: number[] = [];
  for (let i = 0; i * step < max; i++) ticks.push(Math.round(i * step * 1000) / 1000);
  ticks.push(Math.round(ticks.length * step * 1000) / 1000); // always cover max
  return ticks;
}

/** Map values onto a plot box; time on x (evenly spaced when `xs` are indices). */
export function scaleLinear(domainMax: number, rangePx: number): (v: number) => number {
  if (domainMax <= 0) return () => 0;
  return (v: number) => (v / domainMax) * rangePx;
}

/** Polyline path through points. */
export function linePath(points: Pt[]): string {
  if (points.length === 0) return '';
  return points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${r(p.x)} ${r(p.y)}`).join(' ');
}

/** Human duration for axis ticks: ms → "1.2s", "45s", "3m", "1h 04m". */
export function tickDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s < 10 ? s.toFixed(1) : Math.round(s)}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m${Math.round(s % 60) ? ` ${Math.round(s % 60)}s` : ''}`;
  const h = Math.floor(m / 60);
  return `${h}h ${String(m % 60).padStart(2, '0')}m`;
}

function r(n: number): number {
  return Math.round(n * 10) / 10;
}
