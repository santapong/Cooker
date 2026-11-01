import { useEffect, useState } from 'react';

/**
 * useDelayedFlag — true only once `flag` has been true for `delayMs`.
 * Used for skeletons: a fetch that resolves inside the delay never flashes
 * a loading state (spec §3: skeleton appears at 120 ms if still pending).
 */
export function useDelayedFlag(flag: boolean, delayMs: number): boolean {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    if (!flag) {
      setShown(false);
      return;
    }
    const t = window.setTimeout(() => setShown(true), delayMs);
    return () => window.clearTimeout(t);
  }, [flag, delayMs]);
  return shown && flag;
}
