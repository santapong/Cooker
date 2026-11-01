import { createContext } from 'react';

/**
 * Per-scene values a star reads without them living in its node data:
 * the run clock (durations tick every second) and the selected star. Keeping
 * them out of node data keeps node objects stable, so React Flow never
 * re-adopts / re-measures a star just because a second passed.
 */
export interface SceneValue {
  now: number;
  selectedId: string | null;
}

export const SceneContext = createContext<SceneValue>({ now: 0, selectedId: null });
