import { useState } from 'react';
import { Link } from 'react-router-dom';
import { usePageHeading } from '../hooks/usePageHeading';
import { useCatalog } from '../hooks/useCatalog';
import { catalogApi } from '../api/catalog';
import type { CatalogQuery } from '../types/catalog';
import CatalogAppCard from '../components/catalog/CatalogAppCard';
import CatalogFilters from '../components/catalog/CatalogFilters';
import { CatalogPaging, SourceNotices } from '../components/catalog/CatalogShared';
export default function AppsPage() {
  const heading = usePageHeading();
  const [query, setQuery] = useState<CatalogQuery>({ limit: 24, offset: 0 });
  const state = useCatalog(() => catalogApi.apps(query), JSON.stringify(query));
  const items = state.data?.items ?? [];
  return <div className="workspace-page"><header className="workspace-page-head"><div><p className="workspace-eyebrow">Your applications</p><h1 ref={heading} tabIndex={-1}>Apps</h1><p>Cooker apps and selected Vercel projects in one catalog.</p></div><div className="workspace-actions"><button className="hud-btn" disabled={state.loading} onClick={state.refresh}>Refresh</button><Link className="hud-btn hud-btn-primary hud-link" to="/apps/new">＋ New app</Link></div></header><CatalogFilters value={query} onChange={setQuery} providers={[...new Set((state.data?.items ?? []).map((item) => item.provider))]} /><SourceNotices sources={state.data?.sources} />{state.error && <div className="workspace-error" role="alert">{state.error}</div>}{state.loading ? <p role="status">Loading apps…</p> : items.length ? <div className="workspace-card-grid">{items.map((app) => <CatalogAppCard key={app.id} app={app} />)}</div> : !state.error && <div className="workspace-empty"><h2>No apps match this view.</h2><p>Create a Cooker app, or connect Vercel and select projects to synchronize. Try clearing filters if you expected an existing app.</p><Link to="/settings">Manage provider connections →</Link><Link to="/apps/new">Add an app</Link></div>}{state.data && <CatalogPaging total={state.data.total} offset={state.data.offset} limit={state.data.limit || 24} loading={state.loading} onPage={(offset) => setQuery({ ...query, offset })} />}<p className="workspace-note">Vercel projects appear after synchronization. Imported resources are read-only here; native apps retain their existing management actions. Deployment readiness is separate from health.</p></div>;
}
