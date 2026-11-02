import { useCallback, useEffect, useRef, useState } from 'react';
import { pipelineApi } from '../api/pipelines';
import type { Pipeline, PipelineRun, RunStatus, StageApproval } from '../types/pipeline';
import { isTerminal } from '../components/porthole/runState';
import { useWebSocket } from './useWebSocket';
import { readGateSnapshot, type GateLoadState } from '../components/run/approvalPolicy';

export interface UseRunResult {
  pipeline: Pipeline | null;
  run: PipelineRun | null;
  gates: StageApproval[];
  gateState: GateLoadState;
  gateError: string | null;
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
  const [gateState, setGateState] = useState<GateLoadState>('loading');
  const [gateError, setGateError] = useState<string | null>(null);
  const gateRequest = useRef(0);
  const [error, setError] = useState<string | null>(null);
  const loaded = !!pipeline && !!run && run.id === runId;
  const terminal = isTerminal(run?.status);
  const idsRef = useRef({ pipelineId, runId });
  const sourcePending = useRef(false);
  idsRef.current = { pipelineId, runId };

  const refresh = useCallback(async () => {
    const { pipelineId: pid, runId: rid } = idsRef.current;
    if (!pid || !rid) return;
    try {
      const next = await pipelineApi.getRun(pid, rid);
      if (idsRef.current.pipelineId !== pid || idsRef.current.runId !== rid) return;
      setRun(next);
      if (sourcePending.current) {
        const p = await pipelineApi.get(pid);
        if (idsRef.current.pipelineId !== pid || idsRef.current.runId !== rid) return;
        setPipeline(p);
        sourcePending.current = p.stages.length === 0;
      }
    } catch {
      // transient — the next tick retries; the initial load surfaces errors
    }
  }, []);

  const refreshGates = useCallback(async () => {
    const { pipelineId: pid, runId: rid } = idsRef.current;
    if (!pid || !rid) return;
    const request = ++gateRequest.current;
    setGateState('loading');
    setGateError(null);
    const current = () => request === gateRequest.current && idsRef.current.pipelineId === pid && idsRef.current.runId === rid;
    try {
      const res = await pipelineApi.stageApprovals(pid, rid);
      if (!current()) return;
      const next = readGateSnapshot(res, rid);
      if (next === null) throw new Error('Gate snapshot unavailable.');
      setGates(next);
      setGateState('ready');
    } catch {
      if (!current()) return;
      setGateState('unavailable');
      setGateError('Approval gates could not be verified. Refresh before recording a decision.');
    }
  }, []);

  useEffect(() => {
    if (!pipelineId || !runId) return;
    let cancelled = false;
    setError(null);
    setRun(null);
    setGates([]);
    gateRequest.current++;
    setGateState('loading');
    setGateError(null);
    setPipeline(null);
    Promise.all([pipelineApi.get(pipelineId), pipelineApi.getRun(pipelineId, runId)])
      .then(async ([initial, r]) => {
        const p = initial.stages.length === 0 && isTerminal(r.status) ? await pipelineApi.get(pipelineId) : initial;
        if (cancelled) return;
        setPipeline(p);
        sourcePending.current = p.stages.length === 0;
        setRun(r);
      })
      .catch(() => {
        if (!cancelled) setError('This run could not be loaded. Check access and refresh.');
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
  useEffect(() => {
    if (!loaded) return;
    void refreshGates();
    if (terminal) return;
    const t = window.setInterval(() => void refreshGates(), GATE_POLL_MS);
    return () => window.clearInterval(t);
  }, [loaded, terminal, refreshGates]);

  return { pipeline, run, gates, gateState, gateError, error, loaded, terminal, refresh, refreshGates };
}
