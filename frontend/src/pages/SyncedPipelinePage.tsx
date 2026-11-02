import { Link, Navigate, useParams, useSearchParams } from 'react-router-dom';
import { catalogApi } from '../api/catalog';
import { useCatalog } from '../hooks/useCatalog';
import { usePageHeading } from '../hooks/usePageHeading';
import { catalogAppHref, catalogHref } from '../utils/catalog';
import ProviderFlow from '../components/catalog/ProviderFlow';
import { DeploymentHistory, Freshness, SourceNotices } from '../components/catalog/CatalogShared';
import { ServiceGroups } from '../components/catalog/ServiceGroups';
import IntegrationAccess from '../components/catalog/IntegrationAccess';
export default function SyncedPipelinePage() {
  const { catalogId = '' } = useParams();
  const heading = usePageHeading();
  const state = useCatalog(() => catalogApi.pipeline(catalogId), catalogId);
  const detail = state.data;
  const [params, setParams] = useSearchParams();
  const selected = params.get('deployment') ?? '';
  const deployments = [detail?.app?.productionDeployment, detail?.pipeline.latestDeployment, ...(detail?.deployments ?? [])].filter((d, i, all) => d && all.findIndex((v) => v?.id === d.id) === i);
  const deployment = selected ? deployments.find((d) => d?.id === selected) : deployments[0];
  if (detail && detail.pipeline.management !== 'vercel') return <Navigate to={catalogHref(detail.pipeline.href, '/pipelines')} replace />;
  return <div className="workspace-page"><header className="workspace-page-head"><div><p className="workspace-eyebrow">Vercel provider flow</p><h1 ref={heading} tabIndex={-1}>{detail?.pipeline.name ?? 'Provider flow'}</h1><p>A read-only map of source, deployment, and destination.</p></div><button className="hud-btn" onClick={state.refresh} disabled={state.loading}>Refresh</button></header>{state.loading && <p role="status">Loading provider flow…</p>}{state.error && <p className="workspace-error" role="alert">{state.error}</p>}{detail && <><SourceNotices sources={detail.sources} /><Freshness freshness={detail.pipeline.freshness} unavailable={detail.pipeline.sourceUnavailable} />{detail.app && <Link to={catalogAppHref(detail.app)}>Inspect {detail.app.name} →</Link>}<label className="field">Deployment to inspect<select aria-label="Deployment to inspect" className="input" value={selected || deployment?.id || ''} onChange={(e) => setParams({ deployment: e.target.value })}>{selected && !deployment && <option value={selected}>Requested deployment unavailable</option>}{deployments.length ? deployments.map((d) => d && <option key={d.id} value={d.id}>{d.environment || 'Unclassified'} · {d.id} · {d.status}</option>) : <option value="">No deployment records</option>}</select></label>{selected && !deployment ? <p className="workspace-error" role="status">The requested deployment is not in the available records. Choose a recorded deployment above; another deployment’s source will not be substituted.</p> : <ProviderFlow pipeline={detail.pipeline} deployment={deployment} />}<IntegrationAccess inventory={detail.integrations} /><section className="panel"><h2>Services sharing this deployment flow</h2><p className="workspace-note">Production and latest preview records are grouped below. Services are deployed together by the provider; this diagram does not imply independent execution or readiness checks.</p>{detail.services?.length ? <ServiceGroups services={detail.services} /> : <p>No service records available.</p>}</section><DeploymentHistory deployments={detail.deployments} /></>}</div>;
}
