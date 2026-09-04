import { useCallback } from 'react';
import { flushSync } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { useMotionAllowed } from './useMotionAllowed';

type WithViewTransition = Document & {
  startViewTransition?: (update: () => void) => { finished: Promise<void> };
};

/** The shared view-transition name a porthole frame and a flying thumbnail share. */
export const PORTHOLE_TRANSITION_NAME = 'porthole';

/**
 * usePortholeTransition — navigate from a star-chart row into a porthole
 * with rung-3 continuity: the row's thumbnail is named as the shared
 * element and the View Transitions API morphs it into the porthole frame
 * (≤ 400 ms, spec §3). Feature-detected; without the API it is a plain
 * navigation. Under reduced motion / Calm the element is not named, so the
 * transition is the 160 ms root cross-fade defined in porthole.css.
 */
export function usePortholeTransition() {
  const navigate = useNavigate();
  const motion = useMotionAllowed();
  return useCallback(
    (to: string, sharedEl?: Element | null) => {
      const doc = document as WithViewTransition;
      // No API, or a hidden document (transitions are skipped and reject there): plain navigation.
      if (typeof doc.startViewTransition !== 'function' || doc.visibilityState !== 'visible') {
        navigate(to);
        return;
      }
      // Name the thumbnail only when motion is allowed: without a name the
      // transition is the plain root cross-fade (the reduced-motion substitute).
      const el = motion && (sharedEl instanceof HTMLElement || sharedEl instanceof SVGElement) ? sharedEl : null;
      if (el) el.style.setProperty('view-transition-name', PORTHOLE_TRANSITION_NAME);
      const transition = doc.startViewTransition(() => {
        flushSync(() => navigate(to));
      });
      // A skipped/aborted transition rejects `finished`; the navigation has
      // still happened, so just clear the name and stay quiet.
      transition.finished
        .catch(() => undefined)
        .finally(() => {
          if (el) el.style.removeProperty('view-transition-name');
        });
    },
    [navigate, motion],
  );
}
