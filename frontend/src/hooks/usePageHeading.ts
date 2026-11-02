import { useEffect, useRef } from 'react';

/** Keep keyboard navigation oriented after a workspace route opens. */
export function usePageHeading() {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, []);
  return heading;
}
