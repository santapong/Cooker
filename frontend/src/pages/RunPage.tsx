import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { pipelineApi } from '../api/pipelines';
import type { PipelineRun, Pipeline, EnvironmentStatus, StageApproval } from '../types/pipeline';
import { useTheme } from '../theme/ThemeProvider';
import { useUIStore } from '../stores/uiStore';
import { useToastStore } from '../stores/toastStore';
import { useStageLogs } from '../hooks/useStageLogs';
import { capabilitiesApi } from '../api/capabilities';
import StepRail from '../components/run/StepRail';
import LogsPanel from '../components/run/LogsPanel';
import RightRail from '../components/run/RightRail';

export default function RunPage() {
  const t = useTheme();
  const mode = useUIStore((s) => s.mode);
  const pushToast = useToastStore((s) => s.push);
  const { id, runId } = useParams<{ id: string; runId: string }>();

  const [run, setRun] = useState<PipelineRun | null>(null);
  // runRef mirrors run so the approval-poll effect can read the current
  // status without having run in its dep array (which would restart the
  // interval on every getRun response — FE-H2).
  const runRef = useRef<PipelineRun | null>(null);
  useEffect(() => {
    runRef.current = run;
  });
  const [pipeline, setPipeline] = useState<Pipeline | null>(null);
  const [aiTriage, setAiTriage] = useState(false);
  const [envStatuses, setEnvStatuses] = useState<EnvironmentStatus[]>([]);
  // Approval-gate state for StageTypeApproval stages. Polled alongside the
  // run so an awaiting gate surfaces an Approve/Reject affordance and a
  // resolved gate clears it. Keyed by stageId for O(1) rail lookup.
  const [stageGates, setStageGates] = useState<Record<string, StageApproval>>({});
  const [selectedStageId, setSelectedStageId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [cancelling, setCancelling] = useState(false);

  // Live stage-log stream. Backend B1 broadcasts each line on
  //   stage-logs:<runId>:<stageId>
  // useStageLogs handles REST backfill on first paint plus live tail
  // through the existing 60s ws-ticket flow. Drops the previous 3s
  // polling loop entirely.
  const stageLogStream = useStageLogs({
    pipelineId: id ?? '',
    runId: runId ?? '',
    stageId: selectedStageId ?? '',
    enabled: !!(id && runId && selectedStageId),
  });
  const stageLogs = useMemo(() => stageLogStream.lines.join('\n'), [stageLogStream.lines]);
  const logsLoading = !!selectedStageId && !stageLogStream.backfillLoaded;
  const streamTruncated = stageLogStream.streamTruncated;

  useEffect(() => {
    capabilitiesApi
      .get()
      .then((c) => setAiTriage(c.aiTriage))
      .catch(() => setAiTriage(false));
  }, []);

  // Fetch run + pipeline + env status. Refresh env status every 5s
  // to pick up promotions/approvals while you're watching.
  useEffect(() => {
    if (!id || !runId) return;
    Promise.all([pipelineApi.get(id), pipelineApi.getRun(id, runId)])
      .then(([p, r]) => {
        setPipeline(p);
        setRun(r);
        // Default-select the currently running stage, fallback to first.
        const live = r.stageRuns?.find((s) => s.status === 'running');
        setSelectedStageId(live?.stageId ?? p.stages[0]?.id ?? null);
      })
      .catch(() => {
        /* falls through to loading state */
      })
      .finally(() => setLoading(false));
  }, [id, runId]);

  useEffect(() => {
    if (!id || !runId) return;
    let cancelled = false;
    const tick = () => {
      // Stop polling once the run has reached a terminal status (FE-L RunPage).
      // Read from runRef to avoid run being in the dep array.
      const current = runRef.current;
      if (current && current.status !== 'running' && current.status !== 'pending') return;
      pipelineApi
        .envStatus(id, runId)
        .then((s) => {
          if (!cancelled) setEnvStatuses(s ?? []);
        })
        .catch(() => {
          /* env-status is best-effort */
        });
    };
    tick();
    const t = window.setInterval(tick, 5000);
    return () => {
      cancelled = true;
      window.clearInterval(t);
    };
  }, [id, runId]);

  const refreshRun = useCallback(() => {
    if (!id || !runId) return;
    pipelineApi
      .getRun(id, runId)
      .then(setRun)
      .catch(() => {
        /* keep the last good run on a transient error */
      });
  }, [id, runId]);

  // Poll the approval gates + the run while the run is live. The executor
  // blocks an approval stage server-side and broadcasts the decision via
  // the persisted gate; polling lets the run page reflect an awaiting gate
  // (to offer Approve/Reject) and pick up the stage flipping to
  // succeeded/failed once a decision lands. Stops once the run is terminal.
  //
  // run is intentionally NOT in the dep array (FE-H2): including it would
  // restart the interval on every getRun response. Instead we read the
  // current status from runRef (kept in sync above) at tick time.
  useEffect(() => {
    if (!id || !runId) return;
    let cancelled = false;
    const tick = () => {
      // Read from ref so we see the latest run without it being a dep.
      const current = runRef.current;
      if (current && current.status !== 'running' && current.status !== 'pending') return;
      pipelineApi
        .stageApprovals(id, runId)
        .then((res) => {
          if (cancelled) return;
          const byStage: Record<string, StageApproval> = {};
          for (const g of res.gates ?? []) byStage[g.stageId] = g;
          setStageGates(byStage);
        })
        .catch(() => {
          /* gates are best-effort */
        });
      refreshRun();
    };
    tick();
    const t = window.setInterval(tick, 4000);
    return () => {
      cancelled = true;
      window.clearInterval(t);
    };
  }, [id, runId, refreshRun]);

  // Logs are now driven by the useStageLogs hook above (WebSocket
  // stream + REST backfill). The previous 3s-polling loop is gone.

  const cancel = async () => {
    if (!id || !runId) return;
    if (!confirm('Cancel this run?')) return;
    setCancelling(true);
    try {
      await pipelineApi.cancelRun(id, runId);
      pushToast({ kind: 'success', message: 'Cancel requested.' });
      const r = await pipelineApi.getRun(id, runId);
      setRun(r);
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setCancelling(false);
    }
  };

  if (loading) {
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
        Loading run…
      </div>
    );
  }

  const stages = pipeline?.stages ?? [];
  const isLive = run?.status === 'running';

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: mode === 'pro' ? '300px 1fr 320px' : '300px 1fr',
        height: '100%',
      }}
    >
      <StepRail
        run={run}
        stages={stages}
        stageGates={stageGates}
        selectedStageId={selectedStageId}
        onSelect={setSelectedStageId}
        canCancel={isLive}
        cancelling={cancelling}
        onCancel={cancel}
        pipelineId={id ?? ''}
        runId={runId ?? ''}
        onGateResolved={refreshRun}
      />
      <LogsPanel
        stage={stages.find((s) => s.id === selectedStageId) ?? null}
        stageRun={run?.stageRuns?.find((sr) => sr.stageId === selectedStageId) ?? null}
        logs={stageLogs}
        loading={logsLoading}
        run={run}
        streamTruncated={streamTruncated}
        pipelineId={id ?? ''}
        runId={runId ?? ''}
        aiTriage={aiTriage}
      />
      {mode === 'pro' && (
        <RightRail
          pipelineId={id ?? ''}
          runId={runId ?? ''}
          envStatuses={envStatuses}
          onRefresh={() => {
            if (id && runId) {
              pipelineApi.envStatus(id, runId).then((s) => setEnvStatuses(s ?? [])).catch(() => {});
            }
          }}
        />
      )}
    </div>
  );
}
