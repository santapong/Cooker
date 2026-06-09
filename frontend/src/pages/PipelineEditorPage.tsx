import { useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { ReactFlowProvider } from '@xyflow/react';
import PipelineCanvas from '../components/pipeline/PipelineCanvas';
import PipelineToolbar from '../components/pipeline/toolbar/PipelineToolbar';
import NodeConfigPanel from '../components/pipeline/panels/NodeConfigPanel';
import { usePipelineStore } from '../stores/pipelineStore';
import { useUIStore } from '../stores/uiStore';
import { useTheme } from '../theme/ThemeProvider';
import { pipelineApi } from '../api/pipelines';
import { useToastStore } from '../stores/toastStore';
import { Pill } from '../components/ui/atoms';

export default function PipelineEditorPage() {
  const t = useTheme();
  const mode = useUIStore((s) => s.mode);
  const pushToast = useToastStore((s) => s.push);
  const { id } = useParams<{ id: string }>();
  // Per-field selectors: this page legitimately re-renders on pipeline
  // edits (live stage/edge counts in the header) but must not also
  // re-render on nodes/edges churn it never reads (P26-05-25).
  const pipeline = usePipelineStore((s) => s.pipeline);
  const loadPipeline = usePipelineStore((s) => s.loadPipeline);
  const savePipeline = usePipelineStore((s) => s.savePipeline);
  const selectedNodeId = usePipelineStore((s) => s.selectedNodeId);

  useEffect(() => {
    if (id) loadPipeline(id);
  }, [id, loadPipeline]);

  const handleValidate = async () => {
    if (!pipeline) return;
    try {
      const result = await pipelineApi.validate(pipeline.id);
      if (result.valid) {
        pushToast({ kind: 'success', message: 'Pipeline DAG is valid.' });
      } else {
        pushToast({
          kind: 'error',
          message: `Validation failed: ${result.errors.join('; ')}`,
        });
      }
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  const handleRun = async () => {
    if (!pipeline) return;
    try {
      const run = await pipelineApi.run(pipeline.id);
      pushToast({ kind: 'success', message: `Run #${run.id.slice(0, 8)} started.` });
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  if (!pipeline) {
    return (
      <div
        style={{
          height: '100%',
          display: 'grid',
          placeItems: 'center',
          color: t.textMute,
          fontFamily: t.serif,
          fontSize: 18,
        }}
      >
        Loading pipeline…
      </div>
    );
  }

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <div
        style={{
          padding: '12px 18px',
          borderBottom: `1px solid ${t.line}`,
          background: t.surface,
          display: 'flex',
          alignItems: 'center',
          gap: 12,
        }}
      >
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          {pipeline.name}
        </span>
        <Pill>{pipeline.stages.length} steps</Pill>
        <Pill tone="cool">{pipeline.edges.length} edges</Pill>
        <div style={{ flex: 1 }} />
        <span style={{ fontFamily: t.mono, fontSize: 11, color: t.textMute }}>
          {pipeline.id.slice(0, 12)} · updated {new Date(pipeline.updatedAt).toLocaleString()}
        </span>
      </div>

      <div style={{ display: 'flex', flex: 1, minHeight: 0 }}>
        {mode === 'pro' && (
          <PipelineToolbar onSave={savePipeline} onValidate={handleValidate} onRun={handleRun} />
        )}
        <ReactFlowProvider>
          <PipelineCanvas />
        </ReactFlowProvider>
        {selectedNodeId && <NodeConfigPanel />}
      </div>
    </div>
  );
}
