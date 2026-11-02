import { Link, Navigate, useParams } from 'react-router-dom';
import { catalogApi } from '../api/catalog';
import { useCatalog } from '../hooks/useCatalog';
import { usePageHeading } from '../hooks/usePageHeading';
import { catalogAppHref, catalogPipelineHref, deploymentLabel, healthLabel } from '../utils/catalog';
import { DeploymentHistory, DeploymentLinks, DeploymentMetadata, Freshness, ProjectMetadata, SourceNotices } from '../components/catalog/CatalogShared';
import { ServiceGroups } from '../components/catalog/ServiceGroups';
import IntegrationAccess from '../components/catalog/IntegrationAccess';
export default function SyncedAppPage() {
  const { catalogId = '' } = useParams();
  const heading = usePageHeading();
  const state = useCatalog(() => catalogApi.app(catalogId), catalogId);
  const detail = state.data;
  if (detail && detail.app.management !== 'vercel') return <Navigate to={catalogAppHref(detail.app)} replace />;
  return <div className="workspace-page"><header className="workspace-page-head"><div><p className="workspace-eyebrow">Synced app · read-only</p><h1 ref={heading} tabIndex={-1}>{detail?.app.name ?? 'Synced app'}</h1><p>Provider-managed records. Manage deployments and logs in Vercel.</p></div><button className="hud-btn" onClick={state.refresh} disabled={state.loading}>Refresh</button></header>{state.loading && <p role="status">Loading app…</p>}{state.error && <p className="workspace-error" role="alert">{state.error}</p>}{detail && <><SourceNotices sources={detail.sources} /><section className="panel"><div className="workspace-section-head"><h2>Source and ownership</h2><span className="badge">{healthLabel(detail.app.health)}</span></div><p>{detail.app.sourceLabel || detail.app.repository || 'Source unavailable'}</p>{detail.app.artifactSha256 && <p className="catalog-hash">Uploaded bundle SHA-256 <code>{detail.app.artifactSha256}</code></p>}<p>{detail.app.framework || 'Framework not reported'} · {detail.app.provider}</p><ProjectMetadata app={detail.app} /><Freshness freshness={detail.app.freshness} unavailable={detail.app.sourceUnavailable} />{detail.pipeline && <Link to={catalogPipelineHref(detail.pipeline)}>View provider flow →</Link>}</section><section className="panel"><h2>Production deployment</h2>{detail.app.productionDeployment ? <><p>{deploymentLabel(detail.app.productionDeployment.status)} · Health not checked</p><DeploymentMetadata deployment={detail.app.productionDeployment} /><DeploymentLinks deployment={detail.app.productionDeployment} /></> : <p>No production deployment record is available.</p>}<p className="workspace-note">A deployment URL and confirmed production aliases are different addresses. Either may require authentication.</p></section><IntegrationAccess inventory={detail.integrations} /><section className="panel"><h2>Services by deployment</h2><p className="workspace-note">Services below share their provider deployment. They are not independent Cooker deployment stages.</p>{detail.services?.length ? <ServiceGroups services={detail.services} /> : <p>No service records available.</p>}</section><DeploymentHistory deployments={detail.deployments} /></>}</div>;
}
