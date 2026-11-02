import { useCallback, useEffect, useState } from 'react';
import { environmentsApi } from '../../api/environments';
import type { Environment } from '../../types/environment';

/** Public metadata only. A failed provider inventory is not an empty inventory. */
export function useEnvironmentInventory(environmentId: string) {
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [unavailable, setUnavailable] = useState(false);
  const [limit, setLimit] = useState(100);
  const [more, setMore] = useState(false);
  const [selected, setSelected] = useState<Environment | undefined>();
  const [keysUnavailable, setKeysUnavailable] = useState(false);
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const rows: Environment[] = [];
        let hasMore = false;
        for (let offset = 0; offset < limit; offset += 100) {
          const page = await environmentsApi.list({ limit: 100, offset });
          rows.push(...(page ?? [])); hasMore = page?.length === 100;
          if (!hasMore) break;
        }
        if (active) { setEnvironments(rows); setMore(hasMore); setUnavailable(false); }
      } catch { if (active) setUnavailable(true); }
    })();
    return () => { active = false; };
  }, [limit, refresh]);
  useEffect(() => {
    let active = true;
    const row = environments.find((environment) => environment.id === environmentId);
    setSelected(row); setKeysUnavailable(false);
    if (environmentId && row) void environmentsApi.secretKeys(environmentId).then((metadata) => {
      if (!active) return;
      if (metadata.sourceAvailability !== 'available' || !Array.isArray(metadata.keys) || !metadata.keys.every((key) => typeof key === 'string')) { setKeysUnavailable(true); return; }
      setSelected({ ...row, secretKeys: metadata.keys });
    }).catch(() => { if (active) setKeysUnavailable(true); });
    return () => { active = false; };
  }, [environmentId, environments, refresh]);
  const reload = useCallback(() => setRefresh((value) => value + 1), []);
  return { environments, selected: selected?.id === environmentId ? selected : undefined, unavailable, keysUnavailable, more, loadMore: () => setLimit((value) => value + 100), reload };
}
