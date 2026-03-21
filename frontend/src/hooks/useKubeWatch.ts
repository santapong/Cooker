import { useCallback } from 'react';
import { useWebSocket } from './useWebSocket';

export function useKubeWatch(namespace: string, resource: string, onEvent?: (event: unknown) => void) {
  const handleMessage = useCallback((data: unknown) => {
    onEvent?.(data);
  }, [onEvent]);

  return useWebSocket({
    url: `/ws/kubernetes/watch?namespace=${namespace}&resource=${resource}`,
    onMessage: handleMessage,
    autoConnect: !!namespace,
  });
}
