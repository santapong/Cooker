import { Link } from 'react-router-dom';
import type { CatalogApp, CatalogDeployment, CatalogFreshness, CatalogSourceStatus } from '../../types/catalog';
import { catalogHref, deploymentLabel, deploymentURL, vercelInspector } from '../../utils/catalog';
import { timeAgo } from '../../utils/time';
import './catalog.css';
export function SourceNotices({ sources = [] }: { sources?: CatalogSourceStatus[] }) {
  const failed = (sources ?? []).filter((s) => !s.available || s.error);
  return failed.length ? <div className="workspace-error" role="status"><strong>{failed.some((source) => !source.available) ? 'Some sources are unavailable' : 'Some source records need attention'}</strong>{failed.map((s, index) => <p key={s.provider + index}>{s.provider}: {s.error || 'Could not load this source.'}</p>)}<p>Available records remain visible. Counts may be incomplete.</p></div> : null;
}
export function Freshness({ freshness, unavailable }: { freshness?: CatalogFreshness; unavailable?: boolean }) {
  return <p className="workspace-note">{unavailable ? 'Source unavailable · ' : ''}{freshness?.state ? freshness.state.replace(/_/g, ' ') : 'Freshness unavailable'}{freshness?.observedAt ? ' · observed ' + timeAgo(freshness.observedAt) : ''}{freshness?.error ? ' · ' + freshness.error : ''}</p>;
}
export function InspectorLink({ url, label = 'Inspect in Vercel ↗' }: { url?: string; label?: string }) {
  const href = vercelInspector(url);
  return href ? <a href={href} target="_blank" rel="noreferrer">{label}</a> : <span className="workspace-note">Provider link unavailable</span>;
}
export function DeploymentLinks({ deployment }: { deployment: CatalogDeployment }) {
  const url = deploymentURL(deployment.url);
  const aliases = [...new Set(deployment.productionUrls ?? [])].map(deploymentURL).filter((u): u is string => !!u);
  return <div className="catalog-deployment-links">{deployment.href && <Link to={catalogHref(deployment.href, '/runs')}>Open recorded deployment</Link>}{url && <a href={url} target="_blank" rel="noreferrer">Deployment URL ↗</a>}{aliases.length > 0 && <div><strong>{deployment.aliasFreshness && deployment.aliasFreshness.state !== 'up_to_date' ? 'Last-observed production aliases' : 'Confirmed production aliases'}</strong>{aliases.map((alias) => <a key={alias} href={alias} target="_blank" rel="noreferrer">{new URL(alias).hostname} ↗</a>)}</div>}{deployment.aliasFreshness && <div role={deployment.aliasFreshness.error ? 'status' : undefined}><strong className="workspace-note">Production alias observation</strong><Freshness freshness={deployment.aliasFreshness} /></div>}{deployment.provider === 'vercel' && <InspectorLink url={deployment.inspectorUrl} />}</div>;
}
export function DeploymentHistory({ deployments = [] }: { deployments?: CatalogDeployment[] }) {
  const records = deployments ?? [];
  return <section className="panel"><h2>Deployment history</h2><p className="workspace-note">Readiness is a provider deployment result, not a health check or proof of public access. URLs may require authentication.</p>{records.length ? records.map((d) => <article className="catalog-deployment" key={d.id}><div className="workspace-section-head"><h3>{deploymentLabel(d.status)}</h3><span>{d.environment || 'Unclassified environment'}</span></div><p>{d.createdAt ? timeAgo(d.createdAt) : 'Time unavailable'} · <code>{d.id}</code></p>{d.gitBranch && <p>{d.gitBranch}{d.gitCommitSHA ? ' · ' + d.gitCommitSHA.slice(0, 12) : ''}</p>}<DeploymentMetadata deployment={d} /><DeploymentLinks deployment={d} /></article>) : <p>No deployment records available.</p>}</section>;
}
export function CatalogPaging({ total, offset, limit, onPage, loading }: { total: number; offset: number; limit: number; onPage: (offset: number) => void; loading: boolean }) {
  return <div className="catalog-paging"><span>{total ? offset >= total ? `No records on this page · ${total} total` : `${offset + 1}–${Math.min(offset + limit, total)} of ${total}` : '0 records'}</span><button className="hud-btn" disabled={loading || offset === 0} onClick={() => onPage(Math.max(0, offset - limit))}>Previous</button><button className="hud-btn" disabled={loading || offset + limit >= total} onClick={() => onPage(offset + limit)}>Next</button></div>;
}

export function DeploymentMetadata({ deployment }: { deployment: CatalogDeployment }) {
  return deployment.metadataFreshness ? <div role={deployment.metadataFreshness.error ? "status" : undefined}><strong className="workspace-note">Deployment metadata</strong><Freshness freshness={deployment.metadataFreshness} /></div> : null;
}

export function ProjectMetadata({ app }: { app: CatalogApp }) {
  return app.metadataFreshness ? <div role={app.metadataFreshness.error ? "status" : undefined}><strong className="workspace-note">Project metadata</strong><Freshness freshness={app.metadataFreshness} /></div> : null;
}
