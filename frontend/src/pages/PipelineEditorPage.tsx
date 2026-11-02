import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ReactFlowProvider } from '@xyflow/react';
import PipelineCanvas from '../components/pipeline/PipelineCanvas';
import PipelineSourceBinding from '../components/pipeline/PipelineSourceBinding';
import NativeAutomationPanel from '../components/pipeline/NativeAutomationPanel';
import { usePipelineStore } from '../stores/pipelineStore';
import { useDraftMutationFence } from '../components/environment/useDraftMutationFence';
import { useConfigurationDraftStore } from '../stores/configurationDraftStore';
import type { Pipeline } from '../types/pipeline';
import ConfirmButton from '../components/ui/ConfirmButton';
import { configurationError } from '../utils/environmentEditing';
import { pushToast } from '../stores/toastStore';
import { pipelineApi } from '../api/pipelines';
import { validateDAG } from '../utils/dagValidation';
import { useDelayedFlag } from '../hooks/useDelayedFlag';
import { useAuth } from '../auth/OIDCProvider';
import '../components/pipeline/pipeline.css';

export default function PipelineEditorPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const pipeline = usePipelineStore((s) => s.pipeline);
  const dirty = usePipelineStore((s) => s.dirty);
  const loadPipeline = usePipelineStore((s) => s.loadPipeline);
  const savePipeline = usePipelineStore((s) => s.savePipeline);
  const setSource = usePipelineStore((s) => s.setSource);
  const { user } = useAuth();
  const currentMutation = useDraftMutationFence();
  const canEdit = !!user?.roles.some((role) => role === 'admin' || role === 'operator');

  const [error, setError] = useState<string | null>(null);
  const [comparison, setComparison] = useState<{ saved: Pipeline; draft: Pipeline } | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const compareSequence = useRef(0);
  const [busy, setBusy] = useState<'save' | 'validate' | 'run' | null>(null);
  const titleRef = useRef<HTMLHeadingElement>(null);

  const loaded = !!pipeline && pipeline.id === id;
  const showSkeleton = useDelayedFlag(!loaded && !error, 120);

  const invalidateComparison = useCallback(() => { compareSequence.current++; }, []);
  useEffect(() => {
    if (!id) return;
    setError(null); setComparison(null); setSaveError(null); setBusy(null);
    compareSequence.current++;
    const generation = useConfigurationDraftStore.getState().generation;
    loadPipeline(id).catch(() => { if (currentMutation(generation)) setError('Pipeline details are unavailable. Try loading this record again.'); });
    return invalidateComparison;
  }, [id, loadPipeline, invalidateComparison, currentMutation]);

  // Route changes land keyboard focus on the pipeline heading.
  useEffect(() => {
    if (loaded) titleRef.current?.focus({ preventScroll: true });
  }, [loaded]);

  const handleSave = useCallback(async () => {
    if (!canEdit) return;
    const generation = useConfigurationDraftStore.getState().generation;
    setBusy('save');
    try {
      await savePipeline();
      if (!currentMutation(generation)) return;
      setComparison(null); setSaveError(null);
      pushToast('success', 'Pipeline saved.');
    } catch (e) {
      if (currentMutation(generation)) setSaveError(configurationError(e));
    } finally {
      if (currentMutation(generation)) setBusy(null);
    }
  }, [savePipeline, canEdit, currentMutation]);

  const handleValidate = useCallback(async () => {
    if (!pipeline) return;
    const local = validateDAG(pipeline.stages, pipeline.edges);
    if (local.length) {
      pushToast('error', `Invalid DAG: ${local.join('; ')}`);
      return;
    }
    if (dirty) { pushToast('error', 'Save your changes before validating the server definition.'); return; }
    const generation = useConfigurationDraftStore.getState().generation;
    setBusy('validate');
    try {
      const result = await pipelineApi.validate(pipeline.id);
      if (!currentMutation(generation)) return;
      if (result.valid) pushToast('success', 'Pipeline DAG is valid.');
      else pushToast('error', 'The server rejected this definition. Review its configuration and bindings.');
    } catch (e) {
      if (currentMutation(generation)) pushToast('error', configurationError(e));
    } finally {
      if (currentMutation(generation)) setBusy(null);
    }
  }, [pipeline, dirty, currentMutation]);

  const handleRun = useCallback(async () => {
    if (!pipeline || !canEdit) return;
    const generation = useConfigurationDraftStore.getState().generation;
    setBusy('run');
    try {
      if (dirty) await savePipeline();
      const current = usePipelineStore.getState();
      if (!currentMutation(generation) || current.pipeline?.id !== pipeline.id || current.dirty) return;
      const run = await pipelineApi.run(pipeline.id);
      if (!currentMutation(generation)) return;
      pushToast('success', `Run ${run.id.slice(0, 8)} started.`);
      navigate(`/pipelines/${pipeline.id}/runs/${run.id}`);
    } catch (e) {
      if (currentMutation(generation)) pushToast('error', configurationError(e));
    } finally {
      if (currentMutation(generation)) setBusy(null);
    }
  }, [pipeline, dirty, savePipeline, navigate, canEdit, currentMutation]);


  const compareSaved = async () => {
    if (!pipeline || busy) return;
    const draft = pipeline;
    const sequence = ++compareSequence.current;
    setBusy('validate');
    try {
      const saved = await pipelineApi.get(draft.id);
      if (sequence !== compareSequence.current || saved.id !== draft.id || !Number.isSafeInteger(saved.version)) return;
      setComparison({ saved, draft });
    } catch { if (sequence === compareSequence.current) setSaveError('The latest saved definition could not be confirmed. Your draft is retained.'); }
    finally { if (sequence === compareSequence.current) setBusy(null); }
  };

  if (error || !loaded || !pipeline) {
    return <div className="pipeline-editor"><h1 className="hud-title">Pipeline</h1>{error ? <div className="pipeline-load-error" role="alert"><p>{error}</p><button type="button" className="hud-btn" onClick={() => navigate('/pipelines')}>Back to pipelines</button></div> : showSkeleton && <p role="status">Loading pipeline…</p>}</div>;
  }

  const stages = pipeline.stages.length;
  const dependencies = pipeline.edges.length;
  return <div className="pipeline-editor">
    <header className="pipeline-page-heading">
      <div><p className="pipeline-eyebrow">PIPELINE · DEPLOYMENT PLAN</p><h1 ref={titleRef} tabIndex={-1} className="hud-title">{pipeline.name}</h1><p>{stages} {stages === 1 ? 'step' : 'steps'} · {dependencies} {dependencies === 1 ? 'dependency' : 'dependencies'}{pipeline.version !== undefined && ` · Version ${pipeline.version}`}{dirty && <span className="pipeline-unsaved"> · Unsaved changes</span>}</p></div>
      <div className="pipeline-page-actions"><button type="button" className="hud-btn" onClick={handleValidate} disabled={busy !== null}>Validate</button>{canEdit && <><button type="button" className="hud-btn" onClick={handleSave} disabled={busy !== null || !dirty}>{busy === 'save' ? 'Saving…' : 'Save'}</button><button type="button" className="hud-btn hud-btn-primary" onClick={handleRun} disabled={busy !== null || stages === 0}>{busy === 'run' ? 'Starting…' : 'Run pipeline'}</button></>}</div>
    </header>
    {saveError && <p role="alert">{saveError}</p>}
    {canEdit && dirty && <section className="panel" aria-label="Unsaved pipeline definition"><p>Unsaved definitions remain in this session’s memory through navigation and theme changes. Reloading or signing out clears them.</p><button type="button" className="hud-btn" disabled={busy !== null} onClick={() => void compareSaved()}>Compare latest saved definition</button><ConfirmButton className="hud-btn" disabled={busy !== null} confirmLabel="Discard the entire pipeline draft and reload?" onConfirm={async () => { const generation = useConfigurationDraftStore.getState().generation; try { await usePipelineStore.getState().discardDraft(pipeline.id); if (currentMutation(generation)) { setComparison(null); setSaveError(null); } } catch (failure) { if (currentMutation(generation)) setSaveError(configurationError(failure)); } }}>Discard pipeline draft</ConfirmButton>
      {comparison && <><details open><summary>Saved version {comparison.saved.version} compared with draft version {comparison.draft.version ?? 'unknown'}</summary><h3>Latest saved definition</h3><pre className="environment-saved-config">{JSON.stringify(comparison.saved, null, 2)}</pre><h3>Your draft definition</h3><pre className="environment-saved-config">{JSON.stringify(comparison.draft, null, 2)}</pre></details><p>Reapplying uses the entire displayed draft, including source, key and registry selections. It does not merge changed authority or start a deployment; a source-triggered run still requires a fresh review.</p><ConfirmButton className="hud-btn" disabled={busy !== null || comparison.draft !== pipeline} confirmLabel="Reapply this exact draft against the displayed saved version?" onConfirm={() => { const current = usePipelineStore.getState().pipeline; if (current !== comparison.draft || comparison.saved.id !== current?.id) return; usePipelineStore.setState({ pipeline: { ...current, version: comparison.saved.version }, dirty: true }); setComparison(null); setSaveError(null); }}>Use compared saved version</ConfirmButton>{comparison.draft !== pipeline && <p role="alert">Your draft changed after comparison. Compare again before reapplying.</p>}</>}
    </section>}
    <PipelineSourceBinding key={pipeline.id} value={pipeline.source} onChange={setSource} disabled={!canEdit || busy !== null} />
    <NativeAutomationPanel key={`git-${pipeline.id}`} pipeline={pipeline} dirty={dirty} />
    <div className="pipeline-reading-guide"><span aria-hidden="true">A → B</span><div><strong>Start with what each step needs.</strong><p>Follow an arrow from the prerequisite to the step that waits. Select its number for an explanation; a join waits for every incoming requirement.</p></div></div>
    <ReactFlowProvider><PipelineCanvas canEdit={canEdit} /></ReactFlowProvider>
  </div>;
}
