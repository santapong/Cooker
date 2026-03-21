import { useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { ReactFlowProvider } from '@xyflow/react';
import PipelineCanvas from '../components/pipeline/PipelineCanvas';
import PipelineToolbar from '../components/pipeline/toolbar/PipelineToolbar';
import NodeConfigPanel from '../components/pipeline/panels/NodeConfigPanel';
import { usePipelineStore } from '../stores/pipelineStore';
import { pipelineApi } from '../api/pipelines';

export default function PipelineEditorPage() {
  const { id } = useParams<{ id: string }>();
  const { pipeline, loadPipeline, savePipeline, selectedNodeId } = usePipelineStore();

  useEffect(() => {
    if (id) {
      loadPipeline(id);
    }
  }, [id, loadPipeline]);

  const handleValidate = async () => {
    if (!pipeline) return;
    const result = await pipelineApi.validate(pipeline.id);
    if (result.valid) {
      alert('Pipeline is valid!');
    } else {
      alert('Validation errors:\n' + result.errors.join('\n'));
    }
  };

  const handleRun = async () => {
    if (!pipeline) return;
    const run = await pipelineApi.run(pipeline.id);
    alert(`Pipeline run started: ${run.id}`);
  };

  if (!pipeline) {
    return <div style={{ color: '#94a3b8', padding: 40, textAlign: 'center' }}>Loading pipeline...</div>;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: 'calc(100vh - 72px)', margin: -20 }}>
      <PipelineToolbar onSave={savePipeline} onValidate={handleValidate} onRun={handleRun} />
      <div style={{ display: 'flex', flex: 1 }}>
        <ReactFlowProvider>
          <PipelineCanvas />
        </ReactFlowProvider>
        {selectedNodeId && <NodeConfigPanel />}
      </div>
    </div>
  );
}
