import { useCallback, useEffect, useRef, useState } from 'react';
import { pipelineApi } from '../api/pipelines';
import type { Pipeline, PipelineRun, RunStatus, StageApproval } from '../types/pipeline';
import { isTerminal } from '../components/porthole/runState';
import { useWebSocket } from './useWebSocket';

export interface UseRunResult {
  pipeline: Pipeline | null;
  run: PipelineRun | null;
  gates: StageApproval[];
  error: string | null;
  loaded: boolean;
  terminal: boolean;
  refresh: () => Promise<void>;
  refreshGates: () => Promise<void>;
}

const POLL_MS = 3000;
const GATE_POLL_MS = 4000;

/**
 * useRun — one run's live state. Loads the pipeline definition and the run
 * snapshot, applies per-stage status frames from /ws/pipeline-run/:runId as
 * they arrive, and polls the snapshot (timestamps, errors, artifacts) until
 * the run is terminal. Approval gates are polled while an approval stage
 * can still be waiting.
 */
export function useRun(pipelineId: string, runId: string): UseRunResult {
  const [pipeline, setPipeline] = useState<Pipeline | null>(null);
  const [run, setRun] = useState<PipelineRun | null>(null);
  const [gates, setGates] = useState<StageApproval[]>([]);
  const [error, setError] = useState<string | null>(null);
  const loaded = !!pipeline && !!run && run.id === runId;
  const terminal = isTerminal(run?.status);
  const idsRef = useRef({ pipelineId, runId });
  idsRef.current = { pipelineId, runId };

  const refresh = useCallback(async () => {
    const { pipelineId: pid, runId: rid } = idsRef.current;
    if (!pid || !rid) return;
    try {
      const next = await pipelineApi.getRun(pid, rid);
      setRun(next);
    } catch {
      // transient — the next tick retries; the initial load surfaces errors
    }
  }, []);

  const refreshGates = useCallback(async () => {
    const { pipelineId: pid, runId: rid } = idsRef.current;
    if (!pid || !rid) return;
    try {
      const res = await pipelineApi.stageApprovals(pid, rid);
      setGates(res?.gates ?? []);
    } catch {
      /* gates are optional */
    }
  }, []);

  useEffect(() => {
    if (!pipelineId || !runId) return;
    let cancelled = false;
    setError(null);
    setRun(null);
    Promise.all([pipelineApi.get(pipelineId), pipelineApi.getRun(pipelineId, runId)])
      .then(([p, r]) => {
        if (cancelled) return;
        setPipeline(p);
        setRun(r);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [pipelineId, runId]);

  // Live status frames: {nodeId, status} per stage transition.
  const onMessage = useCallback((data: unknown) => {
    const u = data as { nodeId?: string; status?: string };
    if (!u || !u.nodeId || !u.status) return;
    setRun((prev) =>
      prev
        ? {
            ...prev,
            stageRuns: prev.stageRuns.map((sr) =>
              sr.stageId === u.nodeId ? { ...sr, status: u.status as RunStatus } : sr,
            ),
          }
        : prev,
    );
  }, []);
  useWebSocket({
    url: runId ? `/ws/pipeline-run/${runId}` : '',
    autoConnect: !!runId && loaded && !terminal,
    onMessage,
  });

  // Snapshot poll until terminal (fills in timestamps, errors, artifacts, run status).
  useEffect(() => {
    if (!loaded || terminal) return;
    const t = window.setInterval(() => void refresh(), POLL_MS);
    return () => window.clearInterval(t);
  }, [loaded, terminal, refresh]);

  // Approval gates: fetch once when loaded, then poll while a gate can still change.
  const hasApproval = !!pipeline?.stages.some((s) => s.type === 'approval');
  useEffect(() => {
    if (!loaded || !hasApproval) return;
    void refreshGates();
    if (terminal) return;
    const t = window.setInterval(() => void refreshGates(), GATE_POLL_MS);
    return () => window.clearInterval(t);
  }, [loaded, hasApproval, terminal, refreshGates]);

  return { pipeline, run, gates, error, loaded, terminal, refresh, refreshGates };
}
