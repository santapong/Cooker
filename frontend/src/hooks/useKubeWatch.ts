import { useCallback, useRef, useEffect } from 'react';
import { useWebSocket } from './useWebSocket';

export function useKubeWatch(namespace: string, resource: string, onEvent?: (event: unknown) => void) {
  // Stash onEvent in a ref so the useCallback below has an empty dep
  // array and its identity stays stable across renders. The handler reads
  // onEventRef.current at call time so it always invokes the latest
  // callback without causing dep churn (FE-M useKubeWatch).
  const onEventRef = useRef(onEvent);
  useEffect(() => {
    onEventRef.current = onEvent;
  });

  const handleMessage = useCallback((data: unknown) => {
    onEventRef.current?.(data);
  }, []);

  return useWebSocket({
    url: `/ws/kubernetes/watch?namespace=${namespace}&resource=${resource}`,
    onMessage: handleMessage,
    autoConnect: !!namespace,
  });
}
