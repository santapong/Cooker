import { useSearchParams } from 'react-router-dom';
import { catalogApi } from '../api/catalog';
import { useCatalog } from '../hooks/useCatalog';
import { usePageHeading } from '../hooks/usePageHeading';
import type { CatalogQuery } from '../types/catalog';
import CatalogFilters from '../components/catalog/CatalogFilters';
import { CatalogPaging, SourceNotices } from '../components/catalog/CatalogShared';
import { ServiceGroups } from '../components/catalog/ServiceGroups';
export default function ServicesPage() {
  const heading = usePageHeading();
  const [params, setParams] = useSearchParams();
  const offset = Number(params.get('offset') ?? 0);
  const query: CatalogQuery = {
    appId: params.get('appId') ?? '', q: params.get('q') ?? '',
    provider: params.get('provider') ?? '', management: params.get('management') ?? '',
    environment: params.get('environment') ?? 'production_first', status: params.get('status') ?? '',
    limit: 50, offset: Number.isSafeInteger(offset) && offset >= 0 ? offset : 0,
  };
  // One source of truth keeps filtering, pagination, back/reload, and shared links aligned.
  const setQuery = (next: CatalogQuery) => setParams((previous) => {
    const updated = new URLSearchParams(previous);
    for (const key of ['appId', 'q', 'provider', 'management', 'environment', 'status', 'offset'] as const) {
      const value = next[key];
      if (value !== undefined && (key === 'environment' || value !== '' && value !== 0)) updated.set(key, String(value));
      else updated.delete(key);
    }
    return updated;
  });
  const state = useCatalog(() => catalogApi.services(query), JSON.stringify(query));
  const apps = useCatalog(() => catalogApi.apps({ limit: 100 }), 'service-app-options');
  const items = state.data?.items ?? [];
  return <div className="workspace-page"><header className="workspace-page-head"><div><p className="workspace-eyebrow">What runs where</p><h1 ref={heading} tabIndex={-1}>Services</h1><p>Production services first, latest previews next, and native configuration or recorded services separately.</p></div><button className="hud-btn" disabled={state.loading} onClick={() => { state.refresh(); apps.refresh(); }}>Refresh</button></header><CatalogFilters services value={query} onChange={setQuery} providers={items.map((item) => item.provider)} environments={items.map((item) => item.environment)} statuses={items.map((item) => item.status)} /><div className="catalog-filters"><label>App<select aria-label="App" className="input" value={query.appId ?? ''} onChange={(e) => setQuery({ ...query, appId: e.target.value, offset: 0 })}><option value="">All apps</option>{query.appId && !apps.data?.items?.some((a) => a.id === query.appId) && <option value={query.appId}>Selected app</option>}{apps.data?.items?.map((app) => <option key={app.id} value={app.id}>{app.name} · {app.provider}</option>)}</select></label></div>{apps.error && <p className="workspace-note" role="status">App choices are unavailable. Existing service filters still work.</p>}<SourceNotices sources={state.data?.sources} />{state.error && <div className="workspace-error" role="alert">{state.error}</div>}{state.loading ? <p role="status">Loading services…</p> : items.length ? <ServiceGroups services={items} /> : !state.error && <div className="workspace-empty"><h2>No service records in this view.</h2><p>Configured definitions and recorded deployments appear when available. Missing inventory does not mean a service is healthy or stopped.</p></div>}{state.data && <CatalogPaging total={state.data.total} offset={state.data.offset} limit={state.data.limit || 50} loading={state.loading} onPage={(offset) => setQuery({ ...query, offset })} />}<p className="workspace-note">Vercel service inventory follows the production deployment and the latest preview per app. The current page may be a partial inventory. Native recorded or configured services do not imply live runtime inspection. App choices show the first 100 apps.</p></div>;
}
