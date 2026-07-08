import { useEffect, useMemo, useState } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA, CONSOLE } from '../../theme/tokens';
import { Btn, KBD, KindBadge, Pill, SectionLabel } from '../ui/atoms';
import { Starfield } from '../ui/Starfield';
import { useToastStore } from '../../stores/toastStore';
import { pipelineApi } from '../../api/pipelines';
import type { Stage, StageRun, PipelineRun } from '../../types/pipeline';
import StageOutputsTable from './StageOutputsTable';

export default function LogsPanel({
  stage,
  stageRun,
  logs,
  loading,
  run,
  streamTruncated,
  pipelineId,
  runId,
  aiTriage,
}: {
  stage: Stage | null;
  stageRun: StageRun | null;
  logs: string;
  loading: boolean;
  run: PipelineRun | null;
  streamTruncated: boolean;
  pipelineId: string;
  runId: string;
  aiTriage: boolean;
}) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [filter, setFilter] = useState<'all' | 'info' | 'warn' | 'error'>('all');
  const [triaging, setTriaging] = useState(false);
  const [advisory, setAdvisory] = useState<{ text: string; model: string } | null>(null);

  // A new stage selection invalidates the previous advisory.
  useEffect(() => {
    setAdvisory(null);
  }, [stage?.id, runId]);

  const triage = async () => {
    if (!pipelineId || !runId || !stage) return;
    setTriaging(true);
    try {
      const res = await pipelineApi.triageStage(pipelineId, runId, stage.id);
      setAdvisory({ text: res.advisory, model: res.model });
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setTriaging(false);
    }
  };

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
        {aiTriage && stageRun?.status === 'failed' && (
          <Btn onClick={triage} disabled={triaging}>
            {triaging ? 'Asking…' : 'Why did this fail?'}
          </Btn>
        )}
        <KBD>/</KBD>
      </div>

      {advisory && (
        <div
          style={{
            margin: '10px 18px 0',
            padding: '12px 14px',
            border: `1px solid ${t.line}`,
            borderRadius: 10,
            background: t.surface,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
            <SectionLabel>AI triage</SectionLabel>
            <Pill tone="cool">{advisory.model}</Pill>
            <Pill tone="warn">advisory only</Pill>
            <span style={{ flex: 1 }} />
            <Btn onClick={() => setAdvisory(null)}>dismiss</Btn>
          </div>
          <div
            style={{
              whiteSpace: 'pre-wrap',
              fontSize: 12.5,
              lineHeight: 1.6,
              color: t.textSoft,
              maxHeight: 260,
              overflow: 'auto',
            }}
          >
            {advisory.text}
          </div>
        </div>
      )}

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
