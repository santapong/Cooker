import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { ReactFlowProvider } from '@xyflow/react';
import Porthole from '../porthole/Porthole';
import { SceneContext } from '../porthole/sceneContext';
import Badge from '../ui/Badge';
import Caps from '../ui/Caps';
import RunCanvas from './RunCanvas';
import TelemetryConsole from './TelemetryConsole';
import StageRunInspector from './StageRunInspector';
import PromotionPanel from './PromotionPanel';
import { useRun } from '../../hooks/useRun';
import { useStageLogs } from '../../hooks/useStageLogs';
import { useRuntimeLogs } from '../../hooks/useRuntimeLogs';
import { useDelayedFlag } from '../../hooks/useDelayedFlag';
import { pipelineApi } from '../../api/pipelines';
import { pushToast } from '../../stores/toastStore';
import type { AppModel } from '../../types/app';
import type { Pipeline, PipelineRun } from '../../types/pipeline';
import { elapsedMs, formatDuration, runProgress, stageRunMap, statusVariant, telemetryLines } from '../porthole/runState';
import './run.css';

interface Props {
  pipelineId: string;
  runId: string;
  heading: (pipeline: Pipeline, run: PipelineRun) => string;
  /** Deployment view: the app this run deployed (adds URL, health, runtime logs). */
  app?: AppModel | null;
}

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/**
 * RunView — a run in the porthole: stars coloured by stage status, light
 * travelling the constellation, a telemetry console below and a stage
 * inspector to the right. Shared by RunPage and DeploymentPage.
 */
export default function RunView({ pipelineId, runId, heading, app }: Props) {
  const navigate = useNavigate();
  const { pipeline, run, gates, error, loaded, terminal, refresh, refreshGates } = useRun(pipelineId, runId);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [consoleOpen, setConsoleOpen] = useState(true);
  const [consoleMode, setConsoleMode] = useState<'stage' | 'runtime'>('stage');
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
  const selectedStage = selectedId ? (pipeline?.stages.find((s) => s.id === selectedId) ?? null) : null;
  const selectedRun = selectedId ? byId.get(selectedId) : undefined;
  const selectedGate = selectedId ? gates.find((g) => g.stageId === selectedId) : undefined;
  const service = selectedStage?.config.composeServiceName;
  const runtimeMode = !!app && !!service && consoleMode === 'runtime';

  const stageLogs = useStageLogs({ pipelineId, runId, stageId: selectedId ?? '', enabled: !!selectedId && !runtimeMode });
  const runtimeLogs = useRuntimeLogs({ appId: app?.id ?? '', serviceId: service ?? '', enabled: runtimeMode });
  const events = useMemo(() => telemetryLines(run, pipeline?.stages ?? []), [run, pipeline]);

  const onGate = useCallback(
    async (kind: 'approve' | 'reject', note: string) => {
      if (!selectedId) return;
      try {
        if (kind === 'approve') await pipelineApi.approveStage(pipelineId, runId, selectedId, note);
        else await pipelineApi.rejectStage(pipelineId, runId, selectedId, note);
        pushToast('success', kind === 'approve' ? 'Stage approved.' : 'Stage rejected.');
        await Promise.all([refresh(), refreshGates()]);
      } catch (e) {
        pushToast('error', message(e));
      }
    },
    [pipelineId, runId, selectedId, refresh, refreshGates],
  );

  const cancel = async () => {
    setBusy('cancel');
    try {
      await pipelineApi.cancelRun(pipelineId, runId);
      pushToast('info', 'Cancel requested.');
      await refresh();
    } catch (e) {
      pushToast('error', message(e));
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
    } catch (e) {
      pushToast('error', message(e));
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
  const consoleTitle = selectedStage ? (runtimeMode ? `${service} runtime` : selectedStage.name) : 'run';
  const health = app?.healthStatus;

  return (
    <div className="editor">
      <Porthole
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
            <Badge variant={statusVariant(run.status)}>{run.status}</Badge>
            <div className="hud-actions">
              {!terminal && (
                <button type="button" className="hud-btn" onClick={cancel} disabled={busy !== null}>
                  {busy === 'cancel' ? 'Cancelling…' : 'Cancel'}
                </button>
              )}
              {terminal && (
                <button type="button" className="hud-btn hud-btn-primary" onClick={rerun} disabled={busy !== null}>
                  {busy === 'rerun' ? 'Starting…' : '▶ Re-run'}
                </button>
              )}
              {!app && (
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
              <Link className="hud-btn hud-link" to={`/pipelines/${pipelineId}/edit`}>
                Editor
              </Link>
            </div>
          </>
        }
      >
        <div className={consoleOpen ? 'run-canvas console-open' : 'run-canvas'}>
          <ReactFlowProvider>
            <SceneContext.Provider value={scene}>
              <RunCanvas pipeline={pipeline} run={run} onSelect={setSelectedId} layoutKey={consoleOpen ? 'open' : 'closed'} />
            </SceneContext.Provider>
          </ReactFlowProvider>
        </div>
        <TelemetryConsole
          title={consoleTitle}
          open={consoleOpen}
          onToggle={() => setConsoleOpen((v) => !v)}
          events={selectedStage ? undefined : events}
          lines={selectedStage ? (runtimeMode ? runtimeLogs.lines : stageLogs.lines) : undefined}
          live={selectedStage ? (runtimeMode ? runtimeLogs.connected : stageLogs.connected) : !terminal}
          loading={!!selectedStage && !runtimeMode && !stageLogs.backfillLoaded}
          banner={
            stageLogs.streamTruncated
              ? 'Live log stream truncated — reload to refetch.'
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
            app && service ? (
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
            stage={selectedStage}
            stageRun={selectedRun}
            gate={selectedGate}
            now={nowSec}
            onClose={() => setSelectedId(null)}
            onGate={onGate}
            appId={app?.id}
            pipelineId={pipelineId}
            runId={runId}
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
