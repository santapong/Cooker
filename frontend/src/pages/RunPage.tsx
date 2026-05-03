import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { pipelineApi } from '../api/pipelines';
import type { PipelineRun, Pipeline, Stage } from '../types/pipeline';
import { useTheme } from '../theme/ThemeProvider';
import { useUIStore } from '../stores/uiStore';
import { hexA } from '../theme/tokens';
import {
  Btn,
  KBD,
  KindBadge,
  Pill,
  SectionLabel,
  statusTone,
  type Tone,
} from '../components/ui/atoms';
import { Icon } from '../components/ui/Icon';

interface LogLine {
  t: string;
  lvl: 'info' | 'ok' | 'warn' | 'error';
  ch: string;
  msg: string;
}

export default function RunPage() {
  const t = useTheme();
  const mode = useUIStore((s) => s.mode);
  const { id, runId } = useParams<{ id: string; runId: string }>();
  const [run, setRun] = useState<PipelineRun | null>(null);
  const [pipeline, setPipeline] = useState<Pipeline | null>(null);
  const [logs] = useState<LogLine[]>(seedLogs());
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!id || !runId) return;
    Promise.all([pipelineApi.get(id), pipelineApi.getRun(id, runId)])
      .then(([p, r]) => {
        setPipeline(p);
        setRun(r);
      })
      .catch(() => {
        // pipeline / run not found — fall through to loading state
      })
      .finally(() => setLoading(false));
  }, [id, runId]);

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

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: mode === 'pro' ? '300px 1fr 280px' : '300px 1fr',
        height: '100%',
      }}
    >
      <StepRail run={run} stages={stages} />
      <LogsPanel logs={logs} />
      {mode === 'pro' && <TelemetryPanel />}
    </div>
  );
}

function StepRail({ run, stages }: { run: PipelineRun | null; stages: Stage[] }) {
  const t = useTheme();
  const stageRunMap = new Map(run?.stageRuns?.map((s) => [s.stageId, s]) ?? []);
  return (
    <aside
      style={{
        borderRight: `1px solid ${t.line}`,
        background: t.surface,
        overflow: 'auto',
        padding: '16px 0',
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

      <div style={{ position: 'relative', padding: '4px 0' }}>
        <span
          style={{
            position: 'absolute',
            left: 32,
            top: 18,
            bottom: 18,
            width: 1,
            background: t.line,
          }}
        />
        {stages.map((s) => {
          const sr = stageRunMap.get(s.id);
          const tone = statusTone(sr?.status);
          const isCurrent = tone === 'ember';
          return (
            <div
              key={s.id}
              style={{
                display: 'grid',
                gridTemplateColumns: '44px 1fr auto',
                alignItems: 'center',
                gap: 8,
                padding: '10px 18px 10px 14px',
                background: isCurrent ? hexA(t.ember, 0.06) : 'transparent',
                borderLeft: `3px solid ${isCurrent ? t.ember : 'transparent'}`,
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
    </aside>
  );
}

function StepDot({ tone }: { tone: Tone }) {
  const t = useTheme();
  const palette: Record<Tone, string> = {
    good: t.good,
    warn: t.warn,
    bad: t.bad,
    cool: t.cool,
    ember: t.ember,
    accent: t.accent,
    neutral: t.textMute,
  };
  const c = palette[tone];
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
      {tone === 'ember' && (
        <span
          style={{
            width: 8,
            height: 8,
            borderRadius: 999,
            background: c,
            animation: 'cookerPulse 1.4s ease-in-out infinite',
          }}
        />
      )}
      {tone === 'bad' && <Icon name="close" size={12} style={{ color: c }} />}
      {(tone === 'neutral' || tone === 'warn') && (
        <span style={{ width: 6, height: 6, borderRadius: 999, background: t.textMute }} />
      )}
    </div>
  );
}

function LogsPanel({ logs }: { logs: LogLine[] }) {
  const t = useTheme();
  const [filter, setFilter] = useState<'all' | 'info' | 'warn' | 'error'>('all');
  const filtered = logs.filter((l) => {
    if (filter === 'all') return true;
    if (filter === 'info') return l.lvl === 'info' || l.lvl === 'ok';
    return l.lvl === filter;
  });
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
        <KindBadge kind="deploy" />
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: t.text }}>Live logs</div>
          <div style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
            {logs.length.toLocaleString()} lines
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
                background: filter === f ? t.surface : 'transparent',
                color: filter === f ? t.text : t.textMute,
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
          <span style={{ width: 6, height: 6, background: t.good, borderRadius: 999 }} />
          tail · live
        </div>
        <KBD>/</KBD>
      </div>

      <div
        style={{
          flex: 1,
          overflow: 'auto',
          padding: '10px 0',
          fontFamily: t.mono,
          fontSize: 12,
        }}
      >
        {filtered.map((l, i) => {
          const lvlColor = ({ info: t.textSoft, ok: t.good, warn: t.warn, error: t.bad } as const)[l.lvl];
          const chColor = chColorFor(l.ch, t);
          return (
            <div
              key={i}
              style={{
                display: 'grid',
                gridTemplateColumns: '70px 60px 60px 1fr',
                gap: 12,
                padding: '2px 18px',
                lineHeight: 1.55,
              }}
            >
              <span style={{ color: t.textMute }}>{l.t}</span>
              <span
                style={{
                  color: lvlColor,
                  textTransform: 'uppercase',
                  letterSpacing: 0.6,
                  fontSize: 10.5,
                  paddingTop: 1.5,
                }}
              >
                {l.lvl}
              </span>
              <span style={{ color: chColor, fontWeight: 600 }}>[{l.ch}]</span>
              <span style={{ color: t.text }}>{l.msg}</span>
            </div>
          );
        })}
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
              animation: 'cookerPulse 1.2s infinite',
            }}
          />
          <span style={{ animation: 'cookerBlink 1s steps(2) infinite' }}>▌</span>
        </div>
      </div>

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
        <span>{filtered.length} lines</span>
        <span>·</span>
        <span>
          {filtered.filter((l) => l.lvl === 'error').length} errors ·{' '}
          {filtered.filter((l) => l.lvl === 'warn').length} warnings
        </span>
        <span style={{ flex: 1 }} />
        <Btn kind="secondary" icon="pause">
          Pause
        </Btn>
        <Btn kind="ghost" icon="close">
          Cancel
        </Btn>
      </div>
    </section>
  );
}

function TelemetryPanel() {
  const t = useTheme();
  return (
    <aside
      style={{
        borderLeft: `1px solid ${t.line}`,
        background: t.surface,
        overflow: 'auto',
        padding: '16px 18px',
        display: 'flex',
        flexDirection: 'column',
        gap: 16,
      }}
    >
      <SectionLabel>Live telemetry</SectionLabel>
      <Spark label="CPU" value="—" tone="cool" data={[]} />
      <Spark label="Memory" value="—" tone="warn" data={[]} />
      <Spark label="p95 latency" value="—" tone="good" data={[]} />
      <Spark label="req/s" value="—" tone="accent" data={[]} />

      <SectionLabel>Pods</SectionLabel>
      <div style={{ color: t.textMute, fontSize: 12, fontFamily: t.mono, fontStyle: 'italic' }}>
        Telemetry stream not wired up yet.
      </div>
    </aside>
  );
}

function Spark({
  label,
  value,
  data,
  tone,
}: {
  label: string;
  value: string;
  data: number[];
  tone: Tone;
}) {
  const t = useTheme();
  const palette: Record<Tone, string> = {
    good: t.good,
    warn: t.warn,
    bad: t.bad,
    cool: t.cool,
    accent: t.accent,
    ember: t.ember,
    neutral: t.textMute,
  };
  const c = palette[tone];
  const W = 240;
  const H = 36;
  const pts = data.length
    ? data
        .map((d, i) => {
          const max = Math.max(...data);
          const min = Math.min(...data);
          const x = (i / (data.length - 1)) * W;
          const y = H - ((d - min) / Math.max(1, max - min)) * H;
          return `${x},${y}`;
        })
        .join(' ')
    : '';
  return (
    <div>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'baseline',
          marginBottom: 4,
        }}
      >
        <span
          style={{
            fontFamily: t.mono,
            fontSize: 10.5,
            color: t.textMute,
            letterSpacing: 1,
            textTransform: 'uppercase',
          }}
        >
          {label}
        </span>
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          {value}
        </span>
      </div>
      <svg
        width="100%"
        height={H}
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        style={{ display: 'block' }}
      >
        {pts && (
          <>
            <polyline
              points={pts}
              fill="none"
              stroke={c}
              strokeWidth="1.6"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            <polyline points={`${pts} ${W},${H} 0,${H}`} fill={hexA(c, 0.1)} stroke="none" />
          </>
        )}
        {!pts && (
          <line
            x1="0"
            y1={H / 2}
            x2={W}
            y2={H / 2}
            stroke={t.line}
            strokeWidth="1"
            strokeDasharray="4 4"
          />
        )}
      </svg>
    </div>
  );
}

function chColorFor(ch: string, t: { cool: string; accent: string; warn: string; textMute: string }): string {
  if (ch === 'kube') return t.cool;
  if (ch === 'pod') return t.accent;
  if (ch === 'app') return t.warn;
  return t.textMute;
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

function seedLogs(): LogLine[] {
  // Placeholder static log lines — real logs come over the runs WebSocket
  // once that endpoint is wired in. Using sample lines for visual parity.
  return [
    { t: '00:00:01', lvl: 'info', ch: 'app', msg: 'waiting for live log stream…' },
  ];
}
