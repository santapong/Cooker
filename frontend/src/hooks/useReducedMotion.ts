import { useEffect, useState } from 'react';

/**
 * useReducedMotion — true when the user has requested reduced motion.
 * CSS-driven animations are gated via @media in index.css; this hook is for
 * JS/SMIL motion (e.g. the trajectory comet's <animateMotion>) that a CSS
 * media query cannot reach. SSR-safe: defaults to false (motion on).
 */
export function useReducedMotion(): boolean {
  const [reduced, setReduced] = useState(false);
  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return;
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    const update = () => setReduced(mq.matches);
    update();
    mq.addEventListener('change', update);
    return () => mq.removeEventListener('change', update);
  }, []);
  return reduced;
}
