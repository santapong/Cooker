import { useCallback, useEffect, useState } from 'react';
import { pipelineApi } from '../api/pipelines';
import type { Pipeline, PipelineAnalytics } from '../types/pipeline';
import { useTheme } from '../theme/ThemeProvider';
import { Card, PageHeader, Pill, SectionLabel, Select, statusTone } from '../components/ui/atoms';
import { Sparkline } from '../components/ui/Sparkline';

function fmtMs(ms: number): string {
  if (ms <= 0) return '–';
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60_000).toFixed(1)}m`;
}

export default function AnalyticsPage() {
  const t = useTheme();
  const [pipelines, setPipelines] = useState<Pipeline[]>([]);
  const [selected, setSelected] = useState<string>('');
  const [data, setData] = useState<PipelineAnalytics | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    pipelineApi
      .list()
      .then((ps) => {
        setPipelines(ps);
        if (ps.length > 0) setSelected((cur) => cur || ps[0].id);
      })
      .catch(() => setPipelines([]));
  }, []);

  const load = useCallback(() => {
    if (!selected) return;
    setLoading(true);
    pipelineApi
      .analytics(selected, 30)
      .then(setData)
      .catch(() => setData(null))
      .finally(() => setLoading(false));
  }, [selected]);

  useEffect(() => {
    load();
  }, [load]);

  // Series for the sparkline: oldest → newest reads left → right.
  const durationSeries = (data?.runs ?? [])
    .slice()
    .reverse()
    .map((r) => r.durationMs);

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="insights"
        title="Pipeline analytics"
        subtitle="Stage durations and success rates computed from recent run history."
        actions={
          <Select
            value={selected}
            onChange={(e) => setSelected(e.target.value)}
            style={{ width: 260 }}
          >
            {pipelines.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </Select>
        }
      />

      {pipelines.length === 0 ? (
        <Card>
          <div style={{ color: t.textMute, fontFamily: t.serif, fontSize: 16 }}>
            No pipelines yet — create one to see analytics here.
          </div>
        </Card>
      ) : loading || !data ? (
        <Card>
          <div style={{ color: t.textMute, fontFamily: t.mono, fontSize: 12 }}>
            {loading ? 'crunching run history…' : 'no analytics available'}
          </div>
        </Card>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
          <Card style={{ display: 'flex', alignItems: 'center', gap: 18, flexWrap: 'wrap' }}>
            <div>
              <SectionLabel>Runs sampled</SectionLabel>
              <div style={{ fontFamily: t.serif, fontSize: 26, color: t.text }}>{data.runCount}</div>
            </div>
            <div>
              <SectionLabel>Success rate</SectionLabel>
              <div style={{ fontFamily: t.serif, fontSize: 26, color: t.text }}>
                {(data.successRate * 100).toFixed(0)}%
              </div>
            </div>
            <div style={{ flex: 1 }} />
            <div>
              <SectionLabel>Run duration trend (old → new)</SectionLabel>
              <Sparkline values={durationSeries} width={280} height={42} />
            </div>
          </Card>

          <Card pad={0}>
            <div style={{ padding: '14px 18px 6px' }}>
              <SectionLabel>Per-stage durations</SectionLabel>
            </div>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
              <thead>
                <tr style={{ color: t.textMute, fontFamily: t.mono, fontSize: 10.5, textAlign: 'left' }}>
                  {['stage', 'samples', 'p50', 'p95', 'avg', 'success'].map((hd) => (
                    <th key={hd} style={{ padding: '8px 18px', borderBottom: `1px solid ${t.line}` }}>
                      {hd}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {data.stages.map((s) => (
                  <tr key={s.stageId} style={{ borderBottom: `1px solid ${t.line}` }}>
                    <td style={{ padding: '9px 18px', color: t.text }}>
                      {s.name || s.stageId} {s.type && <Pill tone="neutral">{s.type}</Pill>}
                    </td>
                    <td style={{ padding: '9px 18px', fontFamily: t.mono, color: t.textSoft }}>{s.samples}</td>
                    <td style={{ padding: '9px 18px', fontFamily: t.mono, color: t.textSoft }}>{fmtMs(s.p50Ms)}</td>
                    <td style={{ padding: '9px 18px', fontFamily: t.mono, color: t.textSoft }}>{fmtMs(s.p95Ms)}</td>
                    <td style={{ padding: '9px 18px', fontFamily: t.mono, color: t.textSoft }}>{fmtMs(s.avgMs)}</td>
                    <td style={{ padding: '9px 18px' }}>
                      <Pill tone={s.successRate >= 0.9 ? 'good' : s.successRate >= 0.5 ? 'warn' : 'bad'}>
                        {(s.successRate * 100).toFixed(0)}%
                      </Pill>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>

          <Card pad={0}>
            <div style={{ padding: '14px 18px 6px' }}>
              <SectionLabel>Recent runs</SectionLabel>
            </div>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
              <tbody>
                {data.runs.map((r) => (
                  <tr key={r.runId} style={{ borderBottom: `1px solid ${t.line}` }}>
                    <td style={{ padding: '8px 18px', fontFamily: t.mono, color: t.textSoft }}>
                      {r.runId.slice(0, 8)}
                    </td>
                    <td style={{ padding: '8px 18px' }}>
                      <Pill tone={statusTone(r.status)}>{r.status}</Pill>
                    </td>
                    <td style={{ padding: '8px 18px', fontFamily: t.mono, color: t.textSoft }}>
                      {fmtMs(r.durationMs)}
                    </td>
                    <td style={{ padding: '8px 18px', fontSize: 10.5, color: t.textMute }}>
                      {new Date(r.createdAt).toLocaleString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        </div>
      )}
    </div>
  );
}
