import { useEffect, useState } from 'react';
import { pipelineApi } from '../api/pipelines';
import type { Pipeline, PipelineAnalytics } from '../types/pipeline';
import Panel from '../components/ui/Panel';
import Gauge from '../components/instruments/Gauge';
import RunDurationChart from '../components/instruments/RunDurationChart';
import StageDurationChart from '../components/instruments/StageDurationChart';
import { Actions, Field, Select } from '../components/ui/form';
import { formatDuration } from '../components/porthole/runState';
import { shortId, timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

function median(values: number[]): number | null {
  const v = values.filter((n) => n >= 0).sort((a, b) => a - b);
  if (v.length === 0) return null;
  const mid = Math.floor(v.length / 2);
  return v.length % 2 ? v[mid] : (v[mid - 1] + v[mid]) / 2;
}

/** Analytics — stage durations and success rates from recent run history (spec §5.E). */
export default function AnalyticsPage() {
  const [pipelines, setPipelines] = useState<Pipeline[]>([]);
  const [selected, setSelected] = useState('');
  const [data, setData] = useState<PipelineAnalytics | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [table, setTable] = useState(false);

  useEffect(() => {
    pipelineApi
      .list({ limit: 100 })
      .then((list) => {
        setPipelines(list ?? []);
        if (list?.length) setSelected((s) => s || list[0].id);
        else setLoading(false);
      })
      .catch((e: unknown) => {
        setError(message(e));
        setLoading(false);
      });
  }, []);

  useEffect(() => {
    if (!selected) return;
    let cancelled = false;
    setLoading(true);
    pipelineApi
      .analytics(selected, 30)
      .then((a) => {
        if (!cancelled) {
          setData(a);
          setError(null);
        }
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(message(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [selected]);

  const med = data ? median(data.runs.map((r) => r.durationMs)) : null;
  const completed = data?.runs.filter((r) => r.durationMs >= 0).length ?? 0;

  return (
    <div className="detail">
      <header className="detail-head">
        <div className="grow">
          <h1>Analytics</h1>
          <p>Stage durations and success rates computed from the last 30 runs.</p>
        </div>
        <Actions>
          <Field label="Pipeline">
            <Select value={selected} onChange={(e) => setSelected(e.target.value)} options={pipelines.map((p) => ({ value: p.id, label: p.name }))} disabled={!pipelines.length} />
          </Field>
          <button type="button" className="hud-btn" aria-pressed={table} onClick={() => setTable((v) => !v)} style={{ alignSelf: 'flex-end' }}>
            {table ? 'Charts' : 'Table'}
          </button>
        </Actions>
      </header>
      {error && (
        <div className="form-error" role="alert">
          {error}
        </div>
      )}
      {!pipelines.length && !loading && <p>No pipelines yet — analytics appear after the first runs.</p>}
      {data && (
        <>
          <div className="gauges">
            <Gauge label="Runs" value={data.runCount} sub={`${completed} completed`} />
            <Gauge label="Success rate" value={`${Math.round(data.successRate * 100)}%`} tone={data.successRate >= 0.9 ? 'ok' : data.successRate < 0.5 ? 'fail' : 'default'} />
            <Gauge label="Median run" value={med === null ? '—' : formatDuration(med)} />
            <Gauge label="Stages" value={data.stages.length} />
          </div>
          {table ? (
            <Panel title="Runs" aside={`${data.runs.length}`}>
              <table className="chart-table">
                <thead>
                  <tr>
                    <th>Run</th>
                    <th>Status</th>
                    <th>Duration</th>
                    <th>Started</th>
                  </tr>
                </thead>
                <tbody>
                  {data.runs.map((r) => (
                    <tr key={r.runId}>
                      <td>{shortId(r.runId)}</td>
                      <td>{r.status}</td>
                      <td>{r.durationMs >= 0 ? formatDuration(r.durationMs) : '—'}</td>
                      <td>{timeAgo(r.createdAt)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Panel>
          ) : (
            <div className="panel-grid">
              <Panel title="Run duration" aside="last 30 runs" className="panel-span">
                <RunDurationChart runs={data.runs} />
              </Panel>
              <Panel title="Stage duration" aside="median · p95" className="panel-span">
                <StageDurationChart stages={data.stages} />
              </Panel>
            </div>
          )}
        </>
      )}
    </div>
  );
}
