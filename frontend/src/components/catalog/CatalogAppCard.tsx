import { Link } from 'react-router-dom';
import type { CatalogApp } from '../../types/catalog';
import { catalogAppHref, deploymentLabel, healthLabel } from '../../utils/catalog';
import { DeploymentLinks, Freshness, ProjectMetadata } from './CatalogShared';
export default function CatalogAppCard({ app }: { app: CatalogApp }) {
  const provider = app.management === 'vercel';
  const deployment = app.productionDeployment ?? app.latestDeployment;
  return <article className="workspace-app-card catalog-app-card">
    <div className="workspace-card-top"><span className="badge">{provider ? 'Managed by Vercel' : 'Managed by Cooker'}</span><span className="badge">{healthLabel(app.health)}</span></div>
    <h3><Link to={catalogAppHref(app)}>{app.name}</Link></h3>
    <p>{app.sourceLabel || app.repository || 'Source information unavailable'}</p>
    {app.artifactSha256 ? <p className="catalog-hash">Bundle SHA-256 <code>{app.artifactSha256}</code></p> : app.branch && <p className="workspace-repo">{app.branch}</p>}
    <div className="workspace-card-foot"><span>{app.framework || app.provider}</span><span>{deployment?.environment || app.environment || 'Environment unclassified'}</span></div>
    <p>{deploymentLabel(deployment?.status)}</p>
    <ProjectMetadata app={app} />
    <Freshness freshness={app.freshness} unavailable={app.sourceUnavailable} />
    <div className="workspace-card-links"><Link to={catalogAppHref(app)}>{provider || !app.capabilities?.edit ? 'Inspect app →' : 'Manage app →'}</Link>{app.pipelineId && <Link to={provider ? '/pipelines/synced/' + encodeURIComponent(app.pipelineId) : '/pipelines/' + encodeURIComponent(app.pipelineId) + '/edit'}>{provider ? 'View provider flow' : 'View DAG'}</Link>}</div>
    {deployment && <DeploymentLinks deployment={deployment} />}
  </article>;
}
