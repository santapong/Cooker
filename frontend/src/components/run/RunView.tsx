import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { ReactFlowProvider } from '@xyflow/react';
import Porthole from '../porthole/Porthole';
import { SceneContext } from '../porthole/sceneContext';
import Badge from '../ui/Badge';
import Caps from '../ui/Caps';
import RunCanvas from './RunCanvas';
import RunTelemetryConsole from './RunTelemetryConsole';
import StageRunInspector from './StageRunInspector';
import PromotionPanel from './PromotionPanel';
import RunSourceReceipt from './RunSourceReceipt';
import { useRun } from '../../hooks/useRun';
import { useStageLogs } from '../../hooks/useStageLogs';
import { useRuntimeLogs } from '../../hooks/useRuntimeLogs';
import { useDelayedFlag } from '../../hooks/useDelayedFlag';
import { pipelineApi } from '../../api/pipelines';
import { pushToast } from '../../stores/toastStore';
import { useAuth } from '../../auth/OIDCProvider';
import type { AppModel } from '../../types/app';
import type { Pipeline, PipelineRun, Stage } from '../../types/pipeline';
import { confirmedGateDecision, runRolePolicy } from './approvalPolicy';
import { deploymentRuntimeService } from './deploymentRuntime';
import { hasRecordedDockerReceipt, nativeSubjectMatches } from '../../utils/nativeRuntime';
import type { NativeRuntimeStatus } from '../../types/nativeRuntime';
import { elapsedMs, formatDuration, runProgress, stageRunMap, statusVariant, telemetryLines } from '../porthole/runState';
import './run.css';

interface Props {
  pipelineId: string;
  runId: string;
  heading: (pipeline: Pipeline, run: PipelineRun) => string;
  /** Deployment view: the app this run deployed (adds URL, health, runtime logs). */
  app?: AppModel | null;
}

/**
 * RunView — a run in the porthole: stars coloured by stage status, light
 * travelling the constellation, a telemetry console below and a stage
 * inspector to the right. Shared by RunPage and DeploymentPage.
 */
export default function RunView({ pipelineId, runId, heading, app }: Props) {
  const navigate = useNavigate();
  const { user } = useAuth();
  const { canInvoke: canEdit, canApprove } = runRolePolicy(user?.roles);
  const { pipeline, run, gates, gateState, gateError, error, loaded, terminal, refresh, refreshGates } = useRun(pipelineId, runId);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [consoleOpen, setConsoleOpen] = useState(true);
  const [consoleMode, setConsoleMode] = useState<'stage' | 'runtime'>('stage');
  const [nativeRuntime, setNativeRuntime] = useState<NativeRuntimeStatus | null>(null);
  const [promotion, setPromotion] = useState(false);
  const [busy, setBusy] = useState<'cancel' | 'rerun' | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const titleRef = useRef<HTMLHeadingElement>(null);
  const showSkeleton = useDelayedFlag(!loaded && !error, 120);

  // One-second clock while the run is live; frozen once terminal.
  useEffect(() => {
    setNow(Date.now());
    if (!loaded || terminal) return;
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [loaded, terminal]);
  const nowSec = Math.floor(now / 1000) * 1000;

  useEffect(() => {
    if (loaded) titleRef.current?.focus({ preventScroll: true });
  }, [loaded]);
  useEffect(() => {
    setSelectedId(null);
    setConsoleMode('stage');
    setPromotion(false);
  }, [runId]);

  useEffect(() => {
    if (selectedId) setPromotion(false);
  }, [selectedId]);
  const byId = useMemo(() => stageRunMap(run), [run]);
  const selectedRun = selectedId ? byId.get(selectedId) : undefined;
  const selectedGate = selectedId ? gates.find((g) => g.stageId === selectedId) : undefined;
  const currentStage = selectedId ? pipeline?.stages.find((s) => s.id === selectedId) : undefined;
  const retainedDeployment = !app && selectedRun && hasRecordedDockerReceipt(selectedRun.runtimeReceipt, runId, selectedRun.stageId);
  const selectedStage: Stage | null = currentStage ?? (selectedGate ? { id: selectedGate.stageId, name: 'Recorded approval gate', type: 'approval', config: {}, position: { x: 0, y: 0 } } : retainedDeployment ? { id: selectedRun.stageId, name: 'Recorded deployment stage', type: 'deploy', config: {}, position: { x: 0, y: 0 } } : null);
  const runtimeService = deploymentRuntimeService(selectedStage);
  const service = runtimeService?.id;
  const nativeSubject = !app && canEdit && selectedId && nativeRuntime?.binding === 'verified' && nativeSubjectMatches(nativeRuntime.subject, { kind: 'native-stage', pipelineId, runId, stageId: selectedId }) ? nativeRuntime.subject : undefined;
  const supportsRuntimeLogs = app ? !!runtimeService?.canStreamLogs : !!nativeSubject;
  const runtimeMode = supportsRuntimeLogs && consoleMode === 'runtime';
  useEffect(() => {
    if (!supportsRuntimeLogs) setConsoleMode('stage');
  }, [supportsRuntimeLogs]);

  const stageLogs = useStageLogs({ pipelineId, runId, stageId: selectedId ?? '', enabled: !!selectedId && !runtimeMode });
  const runtimeLogs = useRuntimeLogs({ appId: app?.id ?? '', serviceId: service ?? '', enabled: runtimeMode && !!app });
  const events = useMemo(() => telemetryLines(run, pipeline?.stages ?? []), [run, pipeline]);

  const onGate = useCallback(
    async (kind: 'approve' | 'reject', note: string) => {
      if (!canApprove || !selectedId || terminal || gateState !== 'ready' || selectedGate?.status !== 'awaiting' || selectedGate.runId !== run?.id) throw new Error('Gate decision unavailable.');
      try {
        const result = kind === 'approve' ? await pipelineApi.approveStage(pipelineId, runId, selectedId, note) : await pipelineApi.rejectStage(pipelineId, runId, selectedId, note);
        const confirmation = confirmedGateDecision(result, selectedGate, kind);
        if (!confirmation) throw new Error('Gate decision unconfirmed.');
        pushToast('success', confirmation);
        await Promise.all([refresh(), refreshGates()]);
      } catch {
        await refreshGates();
        throw new Error('Gate decision could not be confirmed.');
      }
    },
    [canApprove, pipelineId, runId, selectedId, selectedGate, gateState, run?.id, terminal, refresh, refreshGates],
  );

  const cancel = async () => {
    setBusy('cancel');
    try {
      await pipelineApi.cancelRun(pipelineId, runId);
      pushToast('info', 'Cancel requested.');
      await refresh();
    } catch {
      pushToast('error', 'Cancellation could not be confirmed. Refresh the run before retrying.');
    } finally {
      setBusy(null);
    }
  };

  const rerun = async () => {
    setBusy('rerun');
    try {
      const r = await pipelineApi.run(pipelineId);
      pushToast('success', `Run ${r.id.slice(0, 8)} started.`);
      navigate(`/pipelines/${pipelineId}/runs/${r.id}`);
    } catch {
      pushToast('error', 'A new run could not be confirmed. Refresh before trying again.');
    } finally {
      setBusy(null);
    }
  };

  if (error) {
    return (
      <div className="editor">
        <Porthole title={<Caps as="h2" className="hud-title">Porthole</Caps>}>
          <div className="porthole-empty">
            <div>
              <p>{error}</p>
              <Link className="hud-btn hud-link" to={`/pipelines/${pipelineId}/edit`}>
                Open the editor
              </Link>
            </div>
          </div>
        </Porthole>
      </div>
    );
  }

  if (!loaded || !pipeline || !run) {
    return (
      <div className="editor">
        <Porthole title={<Caps as="h2" className="hud-title">Porthole</Caps>}>
          {showSkeleton && (
            <div role="status" aria-live="polite" aria-label="Loading run" className="porthole-empty">
              <span className="caps">Acquiring telemetry…</span>
            </div>
          )}
        </Porthole>
      </div>
    );
  }

  const progress = runProgress(pipeline.stages, byId);
  const elapsed = elapsedMs(run, nowSec);
  const scene = { now: nowSec, selectedId };
  const consoleTitle = selectedStage ? (runtimeMode ? `${app ? service : selectedStage.name} runtime` : selectedStage.name) : 'run';
  const health = app?.healthStatus;

  return (
    <div className="editor">
      <RunSourceReceipt run={run} currentVersion={pipeline.version} />
      {!app && run.stageRuns.some((stage) => hasRecordedDockerReceipt(stage.runtimeReceipt, runId, stage.stageId)) && <section className="section" aria-label="Recorded deployment stages">
        <p>Inspect the containers recorded by this run. Stage names in the graph come from the current pipeline definition; removed stages keep their recorded IDs.</p>
        <div className="gate-actions">{run.stageRuns.filter((stage) => hasRecordedDockerReceipt(stage.runtimeReceipt, runId, stage.stageId)).map((stage) => <button type="button" key={stage.stageId} className="hud-btn" onClick={() => setSelectedId(stage.stageId)}>Inspect recorded deployment {stage.stageId}</button>)}</div>
      </section>}
      <section className="section" aria-label="Recorded approval gates">
        <p>Approval gates belong to this accepted run, including gates removed from the current pipeline definition.</p>
        {gateState === 'loading' && <p role="status">Checking approval gates…</p>}
        {gateError && <p role="alert">{gateError}</p>}
        {gateState === 'ready' && gates.length === 0 && <p>No recorded approval gates are reported for this run.</p>}
        <div className="gate-actions">{gates.map((gate) => <button type="button" key={gate.id} className="hud-btn" onClick={() => setSelectedId(gate.stageId)}>Inspect recorded gate {gate.stageId} · {gate.status}</button>)}<button type="button" className="hud-btn" disabled={gateState === 'loading'} onClick={() => void refreshGates()}>Refresh approval gates</button></div>
      </section>
      <Porthole
        className="run-porthole"
        starfieldSeed={seedOf(pipelineId)}
        title={
          <h1 ref={titleRef} tabIndex={-1} className="caps hud-title">
            {heading(pipeline, run)}
          </h1>
        }
        hudRight={
          <>
            <span className="mono hud-stats">
              <b>{formatDuration(elapsed)}</b> elapsed · stage <b>{progress.done}</b> / {progress.total}
            </span>
            <Badge variant={statusVariant(run.status)}>{run.status === 'needs_attention' ? 'Needs attention' : run.status}</Badge>
            <div className="hud-actions">
              {canEdit && !terminal && run.status !== 'needs_attention' && (
                <button type="button" className="hud-btn" onClick={cancel} disabled={busy !== null}>
                  {busy === 'cancel' ? 'Cancelling…' : 'Cancel'}
                </button>
              )}
              {canEdit && terminal && !app && (
                <button type="button" className="hud-btn hud-btn-primary" onClick={rerun} disabled={busy !== null}>
                  {busy === 'rerun' ? 'Starting…' : '▶ Re-run'}
                </button>
              )}
              {(canEdit || canApprove) && !app && (
                <button
                  type="button"
                  className="hud-btn"
                  aria-pressed={promotion}
                  onClick={() => {
                    setPromotion((v) => !v);
                    setSelectedId(null);
                  }}
                >
                  Promote
                </button>
              )}
              <Link className="hud-btn hud-link" to={app ? `/apps/${app.id}` : `/pipelines/${pipelineId}/edit`}>
                {app ? 'App settings' : 'Editor'}
              </Link>
            </div>
          </>
        }
      >
        <div className={consoleOpen ? 'run-canvas console-open' : 'run-canvas'}>
          {run.status === 'needs_attention' && <div className="porthole-empty" role="alert"><p>Execution was interrupted. An administrator must check the deployed service and record its outcome before another deployment can run.</p></div>}
          {run.status !== 'needs_attention' && pipeline.stages.length === 0 && <div className="porthole-empty" role="status"><p>{terminal ? run.error || 'Source inspection ended before a deployment graph was created.' : 'Inspecting the source revision… The deployment graph will appear here.'}</p></div>}
          <ReactFlowProvider>
            <SceneContext.Provider value={scene}>
              <RunCanvas pipeline={pipeline} run={run} onSelect={setSelectedId} layoutKey={consoleOpen ? 'open' : 'closed'} />
            </SceneContext.Provider>
          </ReactFlowProvider>
        </div>
        <RunTelemetryConsole
          nativeSubject={runtimeMode ? nativeSubject : undefined}
          title={consoleTitle}
          open={consoleOpen}
          onToggle={() => setConsoleOpen((v) => !v)}
          events={selectedStage ? undefined : events}
          lines={selectedStage ? (runtimeMode ? runtimeLogs.lines : stageLogs.lines) : undefined}
          live={selectedStage ? (runtimeMode ? runtimeLogs.connected : stageLogs.connected) : !terminal}
          loading={!!selectedStage && !runtimeMode && !stageLogs.backfillLoaded}
          banner={
            runtimeMode
              ? app ? 'Live logs follow this app’s current recorded deployment service, not the historical run shown above.' : 'Live logs follow the exact container recorded by this run. Reconnects recheck its identity and accepted credentials.'
              : stageLogs.streamTruncated
              ? 'Some log history is unavailable or has expired. The retained output may be incomplete.'
              : stageLogs.truncated
                ? 'Older lines dropped from the buffer.'
                : null
          }
          trailing={
            <div className="hud-meta mono">
              {run.startedAt && <span>started {new Date(run.startedAt).toLocaleTimeString()}</span>}
              {run.startedByEmail && <span>by {run.startedByEmail}</span>}
              {app?.deployedURL && (
                <a href={app.deployedURL} target="_blank" rel="noreferrer">
                  Open app ↗
                </a>
              )}
              {health && <Badge variant={health === 'healthy' ? 'ok' : health === 'failed' ? 'fail' : 'muted'}>{health}</Badge>}
            </div>
          }
          modeSwitch={
            supportsRuntimeLogs ? (
              <div className="console-mode" role="group" aria-label="Console source">
                <button type="button" aria-pressed={consoleMode === 'stage'} onClick={() => setConsoleMode('stage')}>
                  Stage
                </button>
                <button type="button" aria-pressed={consoleMode === 'runtime'} onClick={() => setConsoleMode('runtime')}>
                  Runtime
                </button>
              </div>
            ) : undefined
          }
        />
        {selectedStage && (
          <StageRunInspector
            key={`${runId}:${selectedId}`}
            stage={selectedStage}
            stageRun={selectedRun}
            gate={selectedGate}
            now={nowSec}
            onClose={() => setSelectedId(null)}
            onGate={canApprove && !terminal ? onGate : undefined}
            gateState={gateState}
            acceptedVersion={run.pipelineVersion}
            currentVersion={pipeline.version}
            retainedGate={!currentStage && !!selectedGate}
            canTriage={canEdit}
            appId={app?.id}
            pipelineId={pipelineId}
            runId={runId}
            onNativeRuntimeStatus={setNativeRuntime}
          />
        )}
        {promotion && !selectedStage && <PromotionPanel pipelineId={pipelineId} runId={runId} terminal={terminal} onClose={() => setPromotion(false)} />}
      </Porthole>
    </div>
  );
}

function seedOf(id: string): number {
  let h = 2166136261;
  for (let i = 0; i < id.length; i++) h = Math.imul(h ^ id.charCodeAt(i), 16777619);
  return (h >>> 0) % 100000;
}
