import { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import { pipelineApi } from '../api/pipelines';
import { useEnvironmentStore } from '../stores/environmentStore';
import type { PipelineRun, Pipeline, Stage, StageRun, EnvironmentStatus, RunDiffReport } from '../types/pipeline';
import { useTheme } from '../theme/ThemeProvider';
import { useUIStore } from '../stores/uiStore';
import { hexA, CONSOLE } from '../theme/tokens';
import {
  Btn,
  KBD,
  KindBadge,
  Pill,
  SectionLabel,
  StatusDot,
  statusTone,
  toneColor,
  type Tone,
} from '../components/ui/atoms';
import { Icon } from '../components/ui/Icon';
import { Starfield } from '../components/ui/Starfield';
import { useToastStore } from '../stores/toastStore';
import { useStageLogs } from '../hooks/useStageLogs';

export default function RunPage() {
  const t = useTheme();
  const mode = useUIStore((s) => s.mode);
  const pushToast = useToastStore((s) => s.push);
  const { id, runId } = useParams<{ id: string; runId: string }>();

  const [run, setRun] = useState<PipelineRun | null>(null);
  const [pipeline, setPipeline] = useState<Pipeline | null>(null);
  const [envStatuses, setEnvStatuses] = useState<EnvironmentStatus[]>([]);
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
        selectedStageId={selectedStageId}
        onSelect={setSelectedStageId}
        canCancel={isLive}
        cancelling={cancelling}
        onCancel={cancel}
      />
      <LogsPanel
        stage={stages.find((s) => s.id === selectedStageId) ?? null}
        stageRun={run?.stageRuns?.find((sr) => sr.stageId === selectedStageId) ?? null}
        logs={stageLogs}
        loading={logsLoading}
        run={run}
        streamTruncated={streamTruncated}
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

function StepRail({
  run,
  stages,
  selectedStageId,
  onSelect,
  canCancel,
  cancelling,
  onCancel,
}: {
  run: PipelineRun | null;
  stages: Stage[];
  selectedStageId: string | null;
  onSelect: (id: string) => void;
  canCancel: boolean;
  cancelling: boolean;
  onCancel: () => void;
}) {
  const t = useTheme();
  const stageRunMap = new Map(run?.stageRuns?.map((s) => [s.stageId, s]) ?? []);
  return (
    <aside
      style={{
        borderRight: `1px solid ${t.line}`,
        background: t.surface,
        overflow: 'auto',
        padding: '16px 0',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div style={{ padding: '0 18px 14px' }}>
        <div
          style={{
            fontFamily: t.mono,
            fontSize: 10.5,
            letterSpacing: 1.2,
            textTransform: 'uppercase',
            color: t.textMute,
          }}
        >
          Run #{run?.id?.slice(0, 8) ?? '—'}
        </div>
        <div
          style={{
            fontFamily: t.serif,
            fontSize: 22,
            fontWeight: 500,
            color: t.text,
            marginTop: 2,
          }}
        >
          {run?.pipelineId?.slice(0, 12) ?? 'pipeline'}
        </div>
        <div style={{ display: 'flex', gap: 6, marginTop: 8, flexWrap: 'wrap' }}>
          <Pill tone={statusTone(run?.status)}>{run?.status ?? 'queued'}</Pill>
          {run?.startedAt && (
            <Pill>{new Date(run.startedAt).toLocaleTimeString()}</Pill>
          )}
        </div>
      </div>

      <div style={{ position: 'relative', padding: '4px 0', flex: 1 }}>
        <span
          style={{
            position: 'absolute',
            left: 32,
            top: 18,
            bottom: 18,
            width: 1,
            background: `linear-gradient(${t.good}, ${t.ember}, ${t.line})`,
          }}
        />
        {stages.map((s) => {
          const sr = stageRunMap.get(s.id);
          const tone = statusTone(sr?.status);
          const isCurrent = tone === 'ember';
          const isSelected = selectedStageId === s.id;
          return (
            <div
              key={s.id}
              onClick={() => onSelect(s.id)}
              style={{
                display: 'grid',
                gridTemplateColumns: '44px 1fr auto',
                alignItems: 'center',
                gap: 8,
                padding: '10px 18px 10px 14px',
                background: isSelected
                  ? hexA(t.accent, 0.06)
                  : isCurrent
                    ? hexA(t.ember, 0.06)
                    : 'transparent',
                borderLeft: `3px solid ${isSelected ? t.accent : isCurrent ? t.ember : 'transparent'}`,
                cursor: 'pointer',
              }}
            >
              <StepDot tone={tone} />
              <div style={{ minWidth: 0 }}>
                <div style={{ fontSize: 12.5, fontWeight: 600, color: t.text }}>{s.name}</div>
                <div
                  style={{
                    fontFamily: t.mono,
                    fontSize: 10.5,
                    color: t.textMute,
                    marginTop: 2,
                  }}
                >
                  {s.type}
                </div>
              </div>
              <span
                style={{
                  fontFamily: t.mono,
                  fontSize: 11,
                  color: isCurrent ? t.ember : t.textMute,
                }}
              >
                {duration(sr?.startedAt, sr?.finishedAt) ?? '—'}
              </span>
            </div>
          );
        })}
        {stages.length === 0 && (
          <div style={{ padding: '20px 18px', color: t.textMute, fontSize: 13 }}>
            This pipeline has no stages yet.
          </div>
        )}
      </div>

      <div
        style={{
          padding: '12px 18px',
          borderTop: `1px solid ${t.line}`,
          display: 'flex',
          gap: 8,
        }}
      >
        <Btn
          kind="danger"
          icon="close"
          onClick={onCancel}
          disabled={!canCancel || cancelling}
          style={{ flex: 1, justifyContent: 'center' }}
        >
          {cancelling ? 'Cancelling…' : 'Cancel run'}
        </Btn>
      </div>
    </aside>
  );
}

function StepDot({ tone }: { tone: Tone }) {
  const t = useTheme();
  const c = toneColor(t, tone);
  return (
    <div
      style={{
        width: 24,
        height: 24,
        borderRadius: 999,
        background: t.bg,
        border: `2px solid ${c}`,
        display: 'grid',
        placeItems: 'center',
        marginLeft: 6,
      }}
    >
      {tone === 'good' && <Icon name="check" size={12} style={{ color: c }} />}
      {tone === 'ember' && <StatusDot tone="ember" pulse size={8} />}
      {tone === 'bad' && <Icon name="close" size={12} style={{ color: c }} />}
      {(tone === 'neutral' || tone === 'warn') && (
        <span style={{ width: 6, height: 6, borderRadius: 999, background: t.textMute }} />
      )}
    </div>
  );
}

function LogsPanel({
  stage,
  stageRun,
  logs,
  loading,
  run,
  streamTruncated,
}: {
  stage: Stage | null;
  stageRun: StageRun | null;
  logs: string;
  loading: boolean;
  run: PipelineRun | null;
  streamTruncated: boolean;
}) {
  const t = useTheme();
  const [filter, setFilter] = useState<'all' | 'info' | 'warn' | 'error'>('all');

  const lines = useMemo(() => {
    const all = logs ? logs.split('\n').map((l) => parseLogLine(l)) : [];
    if (filter === 'all') return all;
    return all.filter((l) => l.level === filter || (filter === 'info' && (l.level === 'info' || l.level === 'ok')));
  }, [logs, filter]);

  return (
    <section
      style={{
        display: 'flex',
        flexDirection: 'column',
        minWidth: 0,
        background: t.bg,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          padding: '10px 18px',
          borderBottom: `1px solid ${t.line}`,
          background: t.surface,
        }}
      >
        <KindBadge kind={stage?.type ?? 'custom'} />
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: t.text }}>
            {stage ? stage.name : 'Select a stage'}
          </div>
          <div style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
            {stage ? `${stage.type} · ${lines.length} lines` : 'no stage selected'}
          </div>
        </div>
        <div
          style={{
            display: 'flex',
            padding: 2,
            background: t.surfaceAlt,
            border: `1px solid ${t.line}`,
            borderRadius: 6,
            fontSize: 10.5,
          }}
        >
          {(['all', 'info', 'warn', 'error'] as const).map((f) => (
            <span
              key={f}
              onClick={() => setFilter(f)}
              style={{
                padding: '4px 10px',
                borderRadius: 4,
                background: filter === f ? hexA(t.violet, 0.18) : 'transparent',
                color: filter === f ? t.violet : t.textMute,
                fontFamily: t.mono,
                textTransform: 'uppercase',
                letterSpacing: 0.6,
                cursor: 'pointer',
                fontWeight: 600,
              }}
            >
              {f}
            </span>
          ))}
        </div>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            fontFamily: t.mono,
            fontSize: 11,
            color: t.textSoft,
            padding: '5px 10px',
            border: `1px solid ${t.line}`,
            borderRadius: 6,
            background: t.bg,
          }}
        >
          <span
            style={{
              width: 6,
              height: 6,
              background: run?.status === 'running' ? t.good : t.textMute,
              borderRadius: 999,
            }}
          />
          {run?.status === 'running' ? 'tail · live' : 'tail · idle'}
        </div>
        <KBD>/</KBD>
      </div>

      {streamTruncated && (
        <div
          style={{
            padding: '7px 18px',
            background: hexA(t.warn, 0.12),
            borderBottom: `1px solid ${hexA(t.warn, 0.35)}`,
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            fontFamily: t.mono,
            fontSize: 11,
            color: t.warn,
          }}
        >
          <Pill tone="warn">stream truncated</Pill>
          live logs truncated — reload to see full history
        </div>
      )}

      <div
        style={{
          flex: 1,
          overflow: 'auto',
          position: 'relative',
          background: CONSOLE.bg,
          fontFamily: t.mono,
          fontSize: 12,
        }}
      >
        <Starfield seed={3} density={26} nebula={false} />
        <div style={{ position: 'relative', padding: '10px 0' }}>
        {loading && lines.length === 0 ? (
          <div style={{ padding: 18, color: t.textMute, fontStyle: 'italic' }}>
            Fetching logs…
          </div>
        ) : !stage ? (
          <div style={{ padding: 18, color: t.textMute, fontStyle: 'italic' }}>
            Click a stage in the rail to see its logs.
          </div>
        ) : lines.length === 0 ? (
          <div style={{ padding: 18, color: t.textMute, fontStyle: 'italic' }}>
            No log lines yet for {stage.name}. Logs will appear as the stage runs.
          </div>
        ) : (
          lines.map((l, i) => {
            const lvlColor =
              l.level === 'ok'
                ? t.good
                : l.level === 'warn'
                  ? t.warn
                  : l.level === 'error'
                    ? t.bad
                    : CONSOLE.dim;
            return (
              <div
                key={i}
                style={{
                  display: 'grid',
                  gridTemplateColumns: '70px 60px 1fr',
                  gap: 12,
                  padding: '2px 18px',
                  lineHeight: 1.55,
                }}
              >
                <span style={{ color: CONSOLE.faint }}>{l.timestamp ?? ''}</span>
                <span
                  style={{
                    color: lvlColor,
                    textTransform: 'uppercase',
                    letterSpacing: 0.6,
                    fontSize: 10.5,
                    paddingTop: 1.5,
                  }}
                >
                  {l.level ?? ''}
                </span>
                <span style={{ color: CONSOLE.text }}>{l.message}</span>
              </div>
            );
          })
        )}
        {run?.status === 'running' && (
          <div
            style={{
              padding: '4px 18px',
              color: t.ember,
              display: 'flex',
              alignItems: 'center',
              gap: 8,
            }}
          >
            <span
              style={{
                width: 6,
                height: 6,
                background: t.ember,
                borderRadius: 999,
                boxShadow: `0 0 8px ${t.ember}`,
                animation: 'ccPulse 1.2s infinite',
              }}
            />
            <span style={{ animation: 'ccBlink 1s steps(2) infinite' }}>▌</span>
          </div>
        )}
        </div>
      </div>

      <StageOutputsTable stageRun={stageRun} />

      <div
        style={{
          padding: '8px 18px',
          borderTop: `1px solid ${t.line}`,
          background: t.surface,
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          fontFamily: t.mono,
          fontSize: 11,
          color: t.textMute,
        }}
      >
        <span>{lines.length} lines</span>
        <span>·</span>
        <span>
          {lines.filter((l) => l.level === 'error').length} errors ·{' '}
          {lines.filter((l) => l.level === 'warn').length} warnings
        </span>
        <span style={{ flex: 1 }} />
        <span>{run?.status ?? 'idle'}</span>
      </div>
    </section>
  );
}

function StageOutputsTable({ stageRun }: { stageRun: StageRun | null }) {
  const t = useTheme();

  const outputs = stageRun?.outputs;
  if (!outputs) return null;

  const visibleEntries = Object.entries(outputs).filter(([k]) => !k.startsWith('_'));
  const isTruncated = '_truncated' in outputs;
  const hasInvalid = '_invalid' in outputs;

  if (visibleEntries.length === 0 && !isTruncated && !hasInvalid) return null;

  return (
    <div
      style={{
        borderTop: `1px solid ${t.line}`,
        background: t.surface,
        padding: '10px 18px',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          marginBottom: visibleEntries.length > 0 ? 8 : 0,
        }}
      >
        <span
          style={{
            fontFamily: t.mono,
            fontSize: 11,
            letterSpacing: 1.4,
            textTransform: 'uppercase',
            color: t.textMute,
          }}
        >
          Outputs
        </span>
        <span style={{ flex: 1, height: 1, background: t.line }} />
        {isTruncated && (
          <Pill tone="warn">outputs truncated</Pill>
        )}
        {hasInvalid && (
          <Pill tone="bad">some outputs rejected</Pill>
        )}
      </div>

      {visibleEntries.length > 0 && (
        <table
          style={{
            width: '100%',
            borderCollapse: 'collapse',
            fontFamily: t.mono,
            fontSize: 11.5,
          }}
        >
          <tbody>
            {visibleEntries.map(([key, value]) => (
              <tr key={key}>
                <td
                  style={{
                    padding: '3px 12px 3px 0',
                    color: t.textMute,
                    whiteSpace: 'nowrap',
                    verticalAlign: 'top',
                    width: '1%',
                  }}
                >
                  {key}
                </td>
                <td
                  style={{
                    padding: '3px 0',
                    color: t.text,
                    wordBreak: 'break-all',
                  }}
                >
                  {/* Coerce defensively: outputs is typed Record<string,string>,
                      but a widened / non-string value (e.g. a rollout object)
                      would otherwise render as [object Object] or throw. */}
                  {String(value)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function RightRail({
  pipelineId,
  runId,
  envStatuses,
  onRefresh,
}: {
  pipelineId: string;
  runId: string;
  envStatuses: EnvironmentStatus[];
  onRefresh: () => void;
}) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const environments = useEnvironmentStore((s) => s.environments);
  const fetchEnvironments = useEnvironmentStore((s) => s.fetchEnvironments);
  const [busy, setBusy] = useState<string | null>(null);
  const [diff, setDiff] = useState<RunDiffReport | null>(null);

  useEffect(() => {
    if (environments.length === 0) fetchEnvironments();
  }, [environments.length, fetchEnvironments]);

  const loadDiff = useCallback(() => {
    if (!pipelineId || !runId) return;
    pipelineApi
      .getRunDiff(pipelineId, runId)
      .then(setDiff)
      .catch(() => setDiff(null));
  }, [pipelineId, runId]);

  useEffect(() => {
    loadDiff();
  }, [loadDiff]);

  const envName = (id: string) => environments.find((e) => e.id === id)?.name ?? id.slice(0, 8);

  const promote = async (toEnvId: string) => {
    setBusy(`promote:${toEnvId}`);
    try {
      await pipelineApi.promoteRun(pipelineId, runId, toEnvId);
      pushToast({ kind: 'success', message: `Promotion to ${envName(toEnvId)} requested.` });
      onRefresh();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(null);
    }
  };

  const approve = async (envId: string) => {
    setBusy(`approve:${envId}`);
    try {
      await pipelineApi.approvePromotion(pipelineId, runId, envId);
      pushToast({ kind: 'success', message: `Approved ${envName(envId)}.` });
      onRefresh();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(null);
    }
  };

  const orderedEnvs = environments.slice().sort((a, b) => a.order - b.order);

  return (
    <aside
      style={{
        borderLeft: `1px solid ${t.line}`,
        background: t.surface,
        overflow: 'auto',
        padding: '16px 18px',
        display: 'flex',
        flexDirection: 'column',
        gap: 18,
      }}
    >
      <div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
          <SectionLabel>Diff vs last green</SectionLabel>
          <span style={{ flex: 1 }} />
          <Btn onClick={loadDiff}>refresh</Btn>
        </div>
        {!diff || diff.reason ? (
          <div style={{ color: t.textMute, fontSize: 12, fontFamily: t.mono, fontStyle: 'italic' }}>
            {diff?.reason ?? 'no diff available'}
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
              <Pill tone="cool">vs {diff.againstRunId?.slice(0, 8)}</Pill>
              {diff.definitionChanged && (
                <Pill tone="warn">
                  definition v{diff.pipelineVersionDelta?.from}→v{diff.pipelineVersionDelta?.to}
                </Pill>
              )}
            </div>
            {diff.stages.map((s) => (
              <div
                key={s.stageId}
                style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 11.5 }}
              >
                <span
                  style={{
                    fontFamily: t.mono,
                    color: t.textSoft,
                    flex: 1,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {s.name || s.stageId}
                </span>
                {s.status.from !== s.status.to && (
                  <Pill tone={statusTone(s.status.to)}>
                    {s.status.from || '–'}→{s.status.to}
                  </Pill>
                )}
                {s.durationMs.from >= 0 && s.durationMs.to >= 0 && s.durationMs.deltaMs !== 0 && (
                  <span
                    style={{
                      fontFamily: t.mono,
                      fontSize: 10.5,
                      color: s.durationMs.deltaMs > 0 ? t.bad : t.good,
                    }}
                  >
                    {s.durationMs.deltaMs > 0 ? '+' : ''}
                    {(s.durationMs.deltaMs / 1000).toFixed(1)}s
                  </span>
                )}
                {s.digest.changed && <Pill tone="cool">digest</Pill>}
              </div>
            ))}
            {diff.variables && (
              <div style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
                vars: +{Object.keys(diff.variables.added ?? {}).length} −
                {Object.keys(diff.variables.removed ?? {}).length} ~
                {Object.keys(diff.variables.changed ?? {}).length}
              </div>
            )}
          </div>
        )}
      </div>

      <SectionLabel>Promotion path</SectionLabel>
      {orderedEnvs.length === 0 ? (
        <div style={{ color: t.textMute, fontSize: 12, fontFamily: t.mono, fontStyle: 'italic' }}>
          No environments configured.
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {orderedEnvs.map((env) => {
            const status = envStatuses.find((s) => s.environmentId === env.id);
            const tone: Tone = status
              ? status.status === 'deployed'
                ? 'good'
                : status.status === 'failed'
                  ? 'bad'
                  : status.status === 'awaiting_approval'
                    ? 'warn'
                    : status.status === 'deploying'
                      ? 'ember'
                      : 'neutral'
              : 'neutral';
            const needsApproval = status?.status === 'awaiting_approval';
            return (
              <div
                key={env.id}
                style={{
                  padding: '10px 12px',
                  background: t.bg,
                  border: `1px solid ${t.line}`,
                  borderRadius: 8,
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 6,
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span style={{ fontFamily: t.mono, fontSize: 12, fontWeight: 600, color: t.text, flex: 1 }}>
                    {env.name}
                  </span>
                  <Pill tone={tone}>{status?.status ?? 'pending'}</Pill>
                </div>
                {status?.promotedAt && (
                  <div style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
                    promoted {new Date(status.promotedAt).toLocaleTimeString()}
                  </div>
                )}
                <div style={{ display: 'flex', gap: 6, marginTop: 4 }}>
                  {needsApproval && (
                    <Btn
                      kind="primary"
                      icon="check"
                      onClick={() => approve(env.id)}
                      disabled={busy !== null}
                      style={{ flex: 1, justifyContent: 'center' }}
                    >
                      {busy === `approve:${env.id}` ? 'Approving…' : 'Approve'}
                    </Btn>
                  )}
                  {!needsApproval && status?.status !== 'deployed' && (
                    <Btn
                      kind="ink"
                      icon="arrow"
                      onClick={() => promote(env.id)}
                      disabled={busy !== null}
                      style={{ flex: 1, justifyContent: 'center' }}
                    >
                      {busy === `promote:${env.id}` ? 'Promoting…' : 'Promote here'}
                    </Btn>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}

      <SectionLabel>Live telemetry</SectionLabel>
      <div style={{ color: t.textMute, fontSize: 12, fontFamily: t.mono, fontStyle: 'italic' }}>
        Telemetry stream not wired up yet (follow-up: per-run WebSocket).
      </div>
    </aside>
  );
}

interface ParsedLogLine {
  timestamp?: string;
  level?: 'info' | 'ok' | 'warn' | 'error';
  message: string;
}

function parseLogLine(raw: string): ParsedLogLine {
  // Best-effort: backend hasn't formalised a log line shape yet, so
  // we try to detect [LEVEL] and HH:MM:SS at the start; otherwise
  // pass the whole line through as the message.
  const trimmed = raw.trim();
  if (!trimmed) return { message: '' };

  // Timestamp HH:MM:SS or HH:MM:SS.mmm at the start.
  const tsMatch = trimmed.match(/^(\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(.*)$/);
  let rest = trimmed;
  let timestamp: string | undefined;
  if (tsMatch) {
    timestamp = tsMatch[1];
    rest = tsMatch[2];
  }

  // [LEVEL] or LEVEL: prefix.
  const lvlMatch = rest.match(/^\[?(INFO|OK|WARN|WARNING|ERROR|ERR)\]?\s*[:\s]\s*(.*)$/i);
  if (lvlMatch) {
    const lvl = lvlMatch[1].toUpperCase();
    const level: ParsedLogLine['level'] =
      lvl === 'INFO' ? 'info' : lvl === 'OK' ? 'ok' : lvl.startsWith('WARN') ? 'warn' : 'error';
    return { timestamp, level, message: lvlMatch[2] };
  }

  return { timestamp, message: rest };
}

function duration(start?: string, end?: string): string | null {
  if (!start) return null;
  const s = new Date(start).getTime();
  const e = end ? new Date(end).getTime() : Date.now();
  const ms = Math.max(0, e - s);
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60_000);
  const sec = Math.floor((ms % 60_000) / 1000);
  return `${m}m ${sec.toString().padStart(2, '0')}s`;
}
