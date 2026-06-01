import { useEffect, useMemo, useState, useCallback } from 'react';
import { useParams } from 'react-router-dom';
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  BackgroundVariant,
  Controls,
  type NodeTypes,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import BuildNode from '../components/pipeline/nodes/BuildNode';
import TestNode from '../components/pipeline/nodes/TestNode';
import DeployNode from '../components/pipeline/nodes/DeployNode';
import PushNode from '../components/pipeline/nodes/PushNode';
import ApprovalNode from '../components/pipeline/nodes/ApprovalNode';
import CustomNode from '../components/pipeline/nodes/CustomNode';
import GroupNode from '../components/pipeline/nodes/GroupNode';
import RuntimeInfoPanel from '../components/pipeline/panels/RuntimeInfoPanel';
import { buildDeploymentLayout } from '../components/pipeline/deploymentLayout';
import { useWebSocket } from '../hooks/useWebSocket';
import { pipelineApi } from '../api/pipelines';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import type { Pipeline, Stage } from '../types/pipeline';

const nodeTypes: NodeTypes = {
  group: GroupNode,
  build: BuildNode,
  test: TestNode,
  deploy: DeployNode,
  push: PushNode,
  approval: ApprovalNode,
  custom: CustomNode,
};

interface SelectedService {
  serviceName: string;
  resources?: { memory?: string; cpus?: string };
}

// DeploymentPage is the read-only grouped deployment DAG view. It loads
// the synthesized compose pipeline, renders services inside group boxes,
// tints them live from the run-status WebSocket, and opens a runtime
// panel (logs + container/pod info) when a service node is clicked.
function DeploymentInner() {
  const t = useTheme();
  const { appId = '', pipelineId = '', runId = '' } = useParams();
  const [pipeline, setPipeline] = useState<Pipeline | null>(null);
  const [statusByStage, setStatusByStage] = useState<Record<string, string>>({});
  const [selected, setSelected] = useState<SelectedService | null>(null);

  useEffect(() => {
    if (!pipelineId) return;
    pipelineApi.get(pipelineId).then(setPipeline).catch(() => setPipeline(null));
  }, [pipelineId]);

  // Live per-stage status: the executor broadcasts {nodeId,status} on
  // the run channel; usePipelineExecution's channel, consumed here
  // directly so we can rebuild the grouped layout + group tints.
  const onStatus = useCallback((data: unknown) => {
    const u = data as { nodeId?: string; status?: string };
    if (u.nodeId && u.status) {
      setStatusByStage((prev) => ({ ...prev, [u.nodeId as string]: u.status as string }));
    }
  }, []);
  useWebSocket({
    url: runId ? `/ws/pipeline-run/${runId}` : '',
    autoConnect: !!runId,
    onMessage: onStatus,
  });

  // Seed statuses from the run snapshot once (covers stages that
  // finished before the socket connected).
  useEffect(() => {
    if (!pipelineId || !runId) return;
    pipelineApi
      .getRun(pipelineId, runId)
      .then((run) => {
        const seed: Record<string, string> = {};
        for (const sr of run.stageRuns || []) seed[sr.stageId] = sr.status;
        setStatusByStage((prev) => ({ ...seed, ...prev }));
      })
      .catch(() => {
        /* run may not exist yet */
      });
  }, [pipelineId, runId]);

  const layout = useMemo(() => {
    if (!pipeline) return { nodes: [], edges: [] };
    return buildDeploymentLayout(pipeline.stages, pipeline.edges, statusByStage);
  }, [pipeline, statusByStage]);

  const stageById = useMemo(() => {
    const m = new Map<string, Stage>();
    pipeline?.stages.forEach((s) => m.set(s.id, s));
    return m;
  }, [pipeline]);

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: { id: string; type?: string }) => {
      if (node.type === 'group') return;
      const stage = stageById.get(node.id);
      const svc = stage?.config?.composeServiceName;
      if (!svc) return;
      setSelected({
        serviceName: svc,
        resources: stage?.config?.resources
          ? { memory: stage.config.resources.memory, cpus: stage.config.resources.cpus }
          : undefined,
      });
    },
    [stageById],
  );

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '10px 16px',
          borderBottom: `1px solid ${t.line}`,
        }}
      >
        <span style={{ fontFamily: t.serif, fontSize: 18, color: t.text }}>
          {pipeline?.name || 'deployment'}
        </span>
        <span style={{ fontFamily: t.mono, fontSize: 11, color: t.textMute }}>
          {pipeline ? `${pipeline.stages.length} stages` : 'loading…'}
        </span>
      </div>
      <div style={{ display: 'flex', flex: 1, minHeight: 0 }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <ReactFlow
            nodes={layout.nodes}
            edges={layout.edges}
            nodeTypes={nodeTypes}
            onNodeClick={onNodeClick}
            fitView
            nodesDraggable={false}
            nodesConnectable={false}
            proOptions={{ hideAttribution: true }}
            style={{ background: t.bg }}
          >
            <Controls
              showInteractive={false}
              style={{ background: t.surface, border: `1px solid ${t.line}`, borderRadius: 8 }}
            />
            <Background variant={BackgroundVariant.Dots} gap={22} size={1} color={hexA(t.text, 0.08)} />
          </ReactFlow>
        </div>
        {selected && (
          <RuntimeInfoPanel
            appId={appId}
            serviceName={selected.serviceName}
            resources={selected.resources}
            onClose={() => setSelected(null)}
          />
        )}
      </div>
    </div>
  );
}

export default function DeploymentPage() {
  return (
    <ReactFlowProvider>
      <DeploymentInner />
    </ReactFlowProvider>
  );
}
