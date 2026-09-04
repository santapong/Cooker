import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ReactFlowProvider } from '@xyflow/react';
import Porthole from '../components/porthole/Porthole';
import PipelineCanvas from '../components/pipeline/PipelineCanvas';
import StageInspector from '../components/pipeline/StageInspector';
import Caps from '../components/ui/Caps';
import { usePipelineStore } from '../stores/pipelineStore';
import { pushToast } from '../stores/toastStore';
import { pipelineApi } from '../api/pipelines';
import { validateDAG } from '../utils/dagValidation';
import { useDelayedFlag } from '../hooks/useDelayedFlag';

/** Faint constellation sketch — the skeleton and the empty state share it. */
function ConstellationSketch({ className, width = 420, height = 200 }: { className?: string; width?: number; height?: number }) {
  return (
    <svg className={className} viewBox="0 0 420 200" width={width} height={height} aria-hidden="true" focusable="false">
      <path d="M 60 110 Q 120 52 180 70 M 60 110 Q 120 148 180 150 M 180 70 Q 235 92 290 100 M 180 150 Q 235 128 290 100 M 290 100 Q 330 84 370 80" />
      {[
        [60, 110],
        [180, 70],
        [180, 150],
        [290, 100],
        [370, 80],
      ].map(([x, y]) => (
        <circle key={`${x}-${y}`} cx={x} cy={y} r="4" />
      ))}
    </svg>
  );
}

export default function PipelineEditorPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const pipeline = usePipelineStore((s) => s.pipeline);
  const dirty = usePipelineStore((s) => s.dirty);
  const selectedNodeId = usePipelineStore((s) => s.selectedNodeId);
  const loadPipeline = usePipelineStore((s) => s.loadPipeline);
  const savePipeline = usePipelineStore((s) => s.savePipeline);
  const addStage = usePipelineStore((s) => s.addStage);

  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<'save' | 'validate' | 'run' | null>(null);
  const titleRef = useRef<HTMLHeadingElement>(null);

  const loaded = !!pipeline && pipeline.id === id;
  const showSkeleton = useDelayedFlag(!loaded && !error, 120);

  useEffect(() => {
    if (!id) return;
    setError(null);
    loadPipeline(id).catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)));
  }, [id, loadPipeline]);

  // Spec §6.3: after the porthole opens, focus lands on the canvas heading.
  useEffect(() => {
    if (loaded) titleRef.current?.focus({ preventScroll: true });
  }, [loaded]);

  const handleSave = useCallback(async () => {
    setBusy('save');
    try {
      await savePipeline();
      pushToast('success', 'Pipeline saved.');
    } catch (e) {
      pushToast('error', e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }, [savePipeline]);

  const handleValidate = useCallback(async () => {
    if (!pipeline) return;
    const local = validateDAG(pipeline.stages, pipeline.edges);
    if (local.length) {
      pushToast('error', `Invalid DAG: ${local.join('; ')}`);
      return;
    }
    setBusy('validate');
    try {
      const result = await pipelineApi.validate(pipeline.id);
      if (result.valid) pushToast('success', 'Pipeline DAG is valid.');
      else pushToast('error', `Validation failed: ${result.errors.join('; ')}`);
    } catch (e) {
      pushToast('error', e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }, [pipeline]);

  const handleRun = useCallback(async () => {
    if (!pipeline) return;
    setBusy('run');
    try {
      if (dirty) await savePipeline();
      const run = await pipelineApi.run(pipeline.id);
      pushToast('success', `Run ${run.id.slice(0, 8)} started.`);
      navigate(`/pipelines/${pipeline.id}/runs/${run.id}`);
    } catch (e) {
      pushToast('error', e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }, [pipeline, dirty, savePipeline, navigate]);

  if (error) {
    return (
      <div className="editor">
        <Porthole title={<Caps as="h2" className="hud-title">Porthole</Caps>}>
          <div className="porthole-empty">
            <div>
              <ConstellationSketch className="chart-skeleton-static" />
              <p>{error}</p>
              <button type="button" className="hud-btn" onClick={() => navigate('/pipelines')}>
                Back to pipelines
              </button>
            </div>
          </div>
        </Porthole>
      </div>
    );
  }

  if (!loaded) {
    return (
      <div className="editor">
        <Porthole title={<Caps as="h2" className="hud-title">Porthole</Caps>}>
          {showSkeleton && (
            <div role="status" aria-live="polite" aria-label="Loading pipeline" className="porthole-empty">
              <ConstellationSketch className="chart-skeleton" width={520} height={260} />
            </div>
          )}
        </Porthole>
      </div>
    );
  }

  const stages = pipeline.stages.length;
  const edges = pipeline.edges.length;

  return (
    <div className="editor">
      <Porthole
        starfieldSeed={hashSeed(pipeline.id)}
        title={
          <h1 ref={titleRef} tabIndex={-1} className="caps hud-title">
            Porthole · {pipeline.name}
          </h1>
        }
        hudRight={
          <>
            <span className="mono hud-stats">
              <b>{stages}</b> {stages === 1 ? 'stage' : 'stages'} · <b>{edges}</b> {edges === 1 ? 'edge' : 'edges'}
              {pipeline.version !== undefined && <> · v{pipeline.version}</>}
              {dirty && <> · <span className="hud-dirty">unsaved</span></>}
            </span>
            <div className="hud-actions">
              <button type="button" className="hud-btn" onClick={handleValidate} disabled={busy !== null}>
                Validate
              </button>
              <button type="button" className="hud-btn" onClick={handleSave} disabled={busy !== null || !dirty}>
                {busy === 'save' ? 'Saving…' : 'Save'}
              </button>
              <button type="button" className="hud-btn hud-btn-primary" onClick={handleRun} disabled={busy !== null || stages === 0}>
                {busy === 'run' ? 'Starting…' : '▶ Run'}
              </button>
            </div>
          </>
        }
      >
        <ReactFlowProvider>
          <PipelineCanvas />
        </ReactFlowProvider>
        {stages === 0 && (
          <div className="porthole-empty">
            <div>
              <ConstellationSketch className="chart-skeleton-static" />
              <p>No stages yet. Drag a stage from the tray, or start with a build.</p>
              <button
                type="button"
                className="hud-btn hud-btn-primary"
                onClick={() => addStage('build', { x: 120, y: 120 })}
              >
                ＋ Add a Build stage
              </button>
            </div>
          </div>
        )}
        {selectedNodeId && <StageInspector />}
      </Porthole>
    </div>
  );
}

/** Stable per-pipeline starfield so the same pipeline always shows the same sky. */
function hashSeed(id: string): number {
  let h = 2166136261;
  for (let i = 0; i < id.length; i++) h = Math.imul(h ^ id.charCodeAt(i), 16777619);
  return (h >>> 0) % 100000;
}
