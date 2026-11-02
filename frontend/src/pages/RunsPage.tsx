import { usePageHeading } from '../hooks/usePageHeading';
import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { pipelineApi } from '../api/pipelines';
import type { Pipeline, PipelineRun } from '../types/pipeline';
import Badge from '../components/ui/Badge';
import { runState, workspaceError } from '../utils/workspace';
import { shortId, timeAgo } from '../utils/time';

export default function RunsPage() {
  const heading = usePageHeading();
  const [params, setParams] = useSearchParams();
  const [pipelines, setPipelines] = useState<Pipeline[]>([]);
  const [listError, setListError] = useState<string | null>(null);
  const [listLoading, setListLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [result, setResult] = useState<{ pipelineId: string; runs: PipelineRun[]; error: string | null; loading: boolean }>({ pipelineId: '', runs: [], error: null, loading: false });
  const selected = params.get('pipeline') ?? pipelines[0]?.id ?? '';
  const pipeline = pipelines.find((p) => p.id === selected);
  const available = !!pipeline;
  useEffect(() => {
    let cancelled = false;
    setListLoading(true);
    setListError(null);
    pipelineApi.list({ limit: 100 }).then((list) => { if (!cancelled) setPipelines(list ?? []); }).catch((e: unknown) => { if (!cancelled) setListError(workspaceError(e)); }).finally(() => { if (!cancelled) setListLoading(false); });
    return () => { cancelled = true; };
  }, [refresh]);
  useEffect(() => {
    if (!selected || !available) return;
    let cancelled = false;
    setResult({ pipelineId: selected, runs: [], error: null, loading: true });
    pipelineApi.listRuns(selected, { limit: 100 }).then((runs) => { if (!cancelled) setResult({ pipelineId: selected, runs: runs ?? [], error: null, loading: false }); }).catch((e: unknown) => { if (!cancelled) setResult({ pipelineId: selected, runs: [], error: workspaceError(e), loading: false }); });
    return () => { cancelled = true; };
  }, [selected, available, refresh]);
  const current = result.pipelineId === selected && available ? result : { runs: [], error: null, loading: available };
  return (
    <div className="workspace-page">
      <header className="workspace-page-head"><div><p className="workspace-eyebrow">Deployment history</p><h1 ref={heading} tabIndex={-1}>Runs</h1><p>Choose a pipeline to see its runs, then open a run for step-by-step progress and logs.</p></div><button className="hud-btn" disabled={listLoading || current.loading} onClick={() => setRefresh((n) => n + 1)}>Refresh</button></header>
      <div className="workspace-toolbar"><label htmlFor="run-pipeline">Pipeline</label><select id="run-pipeline" className="input" value={selected} disabled={listLoading || !pipelines.length} onChange={(e) => setParams({ pipeline: e.target.value })}><option value="" disabled>Choose a pipeline</option>{params.get('pipeline') && !pipeline ? <option value={selected}>Requested pipeline unavailable</option> : null}{pipelines.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select></div>
      {listError || current.error ? <div className="workspace-error" role="alert">{listError ?? current.error}</div> : null}
      {listLoading ? <p role="status">Loading pipelines…</p> : !pipelines.length && !listError ? <div className="workspace-empty"><h2>No pipeline runs yet.</h2><p>Create a pipeline first. Its execution history will appear here after a run starts.</p><Link className="hud-btn hud-btn-primary hud-link" to="/pipelines">Go to pipelines</Link></div> : !pipeline && !listError ? <p>The requested pipeline is unavailable. Choose a pipeline from the list.</p> : current.loading ? <p role="status">Loading runs…</p> : !current.error && !listError ? <section className="panel" aria-labelledby="run-list-title"><div className="workspace-section-head"><h2 id="run-list-title">{pipeline?.name}</h2><span>{current.runs.length} {current.runs.length === 1 ? 'run' : 'runs'}</span></div>{current.runs.length ? <div className="workspace-run-list">{current.runs.map((run) => { const state = runState(run.status); const date = run.startedAt ?? run.createdAt; return <article className="workspace-run-row" key={run.id}><div><Link to={'/pipelines/' + selected + '/runs/' + run.id}>Run {shortId(run.id)} →</Link><p>{date ? timeAgo(date) : 'Start time unavailable'}{run.startedByEmail ? ' · ' + run.startedByEmail : ''}</p>{run.error ? <p>{run.error}</p> : null}</div><Badge variant={state.variant}>{state.label}</Badge><span>{run.stageRuns?.length ?? 0} steps</span></article>; })}</div> : <p>No recorded runs for this pipeline.</p>}</section> : null}
      <p className="workspace-note">Showing up to 100 pipelines and up to 100 runs for the selected pipeline. Refresh to check for updates.</p>
    </div>
  );
}
