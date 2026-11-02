import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { pipelineApi } from '../api/pipelines';
import { catalogApi } from '../api/catalog';
import type { Pipeline, PipelineRun } from '../types/pipeline';
import type { CatalogQuery } from '../types/catalog';
import { ChartRows, type ChartRow, type ChartStatus } from '../components/list/StarChart';
import MiniConstellation from '../components/porthole/MiniConstellation';
import { usePortholeTransition } from '../hooks/usePortholeTransition';
import { useCatalog } from '../hooks/useCatalog';
import { usePageHeading } from '../hooks/usePageHeading';
import { pushToast } from '../stores/toastStore';
import CatalogFilters from '../components/catalog/CatalogFilters';
import { CatalogPaging, DeploymentLinks, Freshness, SourceNotices } from '../components/catalog/CatalogShared';
import { catalogPipelineHref, deploymentLabel } from '../utils/catalog';
/** Native row status remains separate from provider deployment readiness. */
export function runChartStatus(run?: PipelineRun): ChartStatus {
  if (!run) return 'idle';
  if (run.status === 'success') return 'ok';
  if (run.status === 'failed') return 'fail';
  if (run.status === 'running' || run.stageRuns?.some((s) => s.status === 'running')) return 'running';
  return 'idle';
}
export default function PipelinesPage() {
  const heading = usePageHeading();
  const open = usePortholeTransition();
  const navigate = useNavigate();
  const [query, setQuery] = useState<CatalogQuery>({ limit: 50, offset: 0 });
  const state = useCatalog(() => catalogApi.pipelines(query), JSON.stringify(query));
  const [definitions, setDefinitions] = useState<Record<string, Pipeline>>({});
  const [runs, setRuns] = useState<Record<string, PipelineRun>>({});
  const [definitionError, setDefinitionError] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    setDefinitions({}); setRuns({}); setDefinitionError(false);
    const native = (state.data?.items ?? []).filter((p) => p.kind === 'cooker-dag' && p.nativePipelineId);
    Promise.all(native.map(async (p) => {
      const [definition, history] = await Promise.allSettled([pipelineApi.get(p.nativePipelineId!), pipelineApi.listRuns(p.nativePipelineId!, { limit: 1 })]);
      return { id: p.nativePipelineId!, definition: definition.status === 'fulfilled' ? definition.value : undefined, run: history.status === 'fulfilled' ? history.value?.[0] : undefined, failed: definition.status === 'rejected' || history.status === 'rejected' };
    })).then((items) => {
      if (!active) return;
      setDefinitions(Object.fromEntries(items.filter((p) => p.definition).map((p) => [p.id, p.definition!])));
      setRuns(Object.fromEntries(items.filter((p) => p.run).map((p) => [p.id, p.run!])));
      setDefinitionError(items.some((p) => p.failed));
    });
    return () => { active = false; };
  }, [state.data]);
  const create = useCallback(async (event: FormEvent) => {
    event.preventDefault();
    const name = newName.trim(); if (!name || busy) return;
    setBusy(true);
    try { const pipeline = await pipelineApi.create({ name, stages: [], edges: [], variables: {} }); pushToast('success', `Pipeline "${pipeline.name}" created.`); open(`/pipelines/${pipeline.id}/edit`, null); }
    catch (error) { pushToast('error', error instanceof Error ? error.message : String(error)); setBusy(false); }
  }, [newName, busy, open]);
  const native = (state.data?.items ?? []).filter((p) => p.kind === 'cooker-dag');
  const imported = (state.data?.items ?? []).filter((p) => p.kind === 'provider-flow');
  const rows: ChartRow[] = native.map((p) => {
    const definition = p.nativePipelineId ? definitions[p.nativePipelineId] : undefined;
    const href = catalogPipelineHref(p);
    const run = p.nativePipelineId ? runs[p.nativePipelineId] : undefined;
    return { id: p.id, name: p.name, status: runChartStatus(run), href, onOpen: (element) => open(href, element), thumb: definition ? <MiniConstellation stages={definition.stages ?? []} edges={definition.edges ?? []} statuses={new Map((run?.stageRuns ?? []).map((stage) => [stage.stageId, stage.status]))} /> : undefined, meta: [p.stages !== undefined || definition ? `${p.stages ?? definition?.stages?.length} stages` : 'Stage count unavailable', 'Cooker DAG', deploymentLabel(p.latestDeployment?.status)], trailing: p.latestDeployment ? <DeploymentLinks deployment={p.latestDeployment} /> : undefined };
  });
  return <div className="workspace-page"><header className="workspace-page-head"><div><p className="workspace-eyebrow">Deployment plans and provider flows</p><h1 ref={heading} tabIndex={-1}>Pipelines</h1><p>Edit native Cooker DAGs or inspect how Vercel connects source to a deployment.</p></div><div className="workspace-actions"><button className="hud-btn" disabled={state.loading} onClick={state.refresh}>Refresh</button>{!creating && <button className="hud-btn hud-btn-primary" onClick={() => setCreating(true)}>＋ New pipeline</button>}</div></header>{creating && <form className="chart-form" onSubmit={create}><input className="input" autoFocus aria-label="New pipeline name" value={newName} onChange={(e) => setNewName(e.target.value)} disabled={busy} /><button className="hud-btn hud-btn-primary" disabled={busy || !newName.trim()}>{busy ? 'Creating…' : 'Create'}</button><button type="button" className="hud-btn" disabled={busy} onClick={() => setCreating(false)}>Cancel</button></form>}<CatalogFilters pipelines value={query} onChange={setQuery} providers={[...new Set((state.data?.items ?? []).map((item) => item.provider))]} /><SourceNotices sources={state.data?.sources} />{state.error && <p className="workspace-error" role="alert">{state.error}</p>}{state.loading ? <p role="status">Loading pipelines…</p> : <>{native.length > 0 && <section><h2>Cooker DAGs</h2><p className="workspace-note">Native definitions retain their editor, run, and approval controls.</p>{definitionError && <p role="status">Some graph previews are unavailable. Open the pipeline to inspect it.</p>}<ChartRows rows={rows} /></section>}{imported.length > 0 && <section><h2>Vercel provider flows</h2><p className="workspace-note">Read-only relationships reported by the provider. These are not Cooker executable DAGs.</p><div className="catalog-provider-rows">{imported.map((p) => <article className="catalog-provider-row" key={p.id}><div className="workspace-section-head"><h3><Link to={catalogPipelineHref(p)}>{p.name}</Link></h3><span className="badge">Managed by Vercel</span></div><p>{p.sourceLabel || p.repository || 'Source unavailable'}</p><p>{deploymentLabel(p.latestDeployment?.status)} · Health not checked</p><Freshness freshness={p.freshness} unavailable={p.sourceUnavailable} /><button className="hud-btn" onClick={() => navigate(catalogPipelineHref(p))}>Inspect provider flow →</button>{p.latestDeployment && <DeploymentLinks deployment={p.latestDeployment} />}</article>)}</div></section>}{!native.length && !imported.length && !state.error && <div className="workspace-empty"><h2>No pipelines match this view.</h2><p>Create a native pipeline or synchronize a selected Vercel project.</p></div>}</>}{state.data && <CatalogPaging total={state.data.total} offset={state.data.offset} limit={state.data.limit || 50} loading={state.loading} onPage={(offset) => setQuery({ ...query, offset })} />}</div>;
}
