import { useCallback } from 'react';
import { useWebSocket } from './useWebSocket';
import { usePipelineStore } from '../stores/pipelineStore';

export function usePipelineExecution(runId: string | null) {
  const store = usePipelineStore();

  const handleMessage = useCallback((data: unknown) => {
    const update = data as { nodeId: string; status: string };
    if (update.nodeId && update.status) {
      // Update node status in the store for live visualization
      const nodes = store.nodes.map((node) =>
        node.id === update.nodeId
          ? { ...node, data: { ...node.data, status: update.status } }
          : node
      );
      store.setNodes(nodes);
    }
  }, [store]);

  const ws = useWebSocket({
    url: runId ? `/ws/pipeline-run/${runId}` : '',
    onMessage: handleMessage,
    autoConnect: !!runId,
  });

  return {
    connected: ws.connected,
    disconnect: ws.disconnect,
  };
}
