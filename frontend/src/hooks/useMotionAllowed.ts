import { useUIStore } from '../stores/uiStore';
import { useReducedMotion } from './useReducedMotion';

/**
 * useMotionAllowed — the single gate for JS-driven motion (WAAPI drag
 * settle, View Transitions, SMIL). False when the OS asks for reduced
 * motion OR the user switched Calm mode on. CSS animations do not need
 * this: they carry their own `@media (prefers-reduced-motion)` and
 * `[data-calm="true"]` branches.
 */
export function useMotionAllowed(): boolean {
  const reduced = useReducedMotion();
  const calm = useUIStore((s) => s.calmMode);
  return !reduced && !calm;
}
