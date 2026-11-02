import { useEffect, useState } from 'react';
import { workspaceError } from '../utils/workspace';
/** Request identity fences late responses; changing filters never displays an older result as current. */
export function useCatalog<T>(load: () => Promise<T>, key: string) {
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{ key: string; data?: T; error?: string; loading: boolean }>({ key, loading: true });
  useEffect(() => {
    let active = true;
    setState({ key, loading: true });
    load().then((data) => { if (active) setState({ key, data, loading: false }); }).catch((error: unknown) => { if (active) setState({ key, error: workspaceError(error), loading: false }); });
    return () => { active = false; };
    // load is intentionally keyed by the complete request identity, not a render-created closure.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, revision]);
  return { ...(state.key === key ? state : { key, loading: true }), refresh: () => setRevision((n) => n + 1) };
}
