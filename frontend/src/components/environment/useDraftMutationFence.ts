import { useCallback, useEffect, useRef } from 'react';
import { useLocation } from 'react-router-dom';
import { useConfigurationDraftStore } from '../../stores/configurationDraftStore';
/** A departed page or signed-out session cannot continue a mutation chain. */
export function useDraftMutationFence() {
  const { key: locationKey } = useLocation();
  const coordinate = useRef({ key: locationKey, epoch: 0 });
  if (coordinate.current.key !== locationKey) coordinate.current = { key: locationKey, epoch: coordinate.current.epoch + 1 };
  const epoch = coordinate.current.epoch;
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  return useCallback((generation: number) => mounted.current && coordinate.current.epoch === epoch && generation === useConfigurationDraftStore.getState().generation, [epoch]);
}
