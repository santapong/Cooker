import { Link } from 'react-router-dom';
import type { AppModel, AppDeployRecord } from '../../types/app';
import type { Pipeline } from '../../types/pipeline';
import Badge from '../ui/Badge';
import { AppsIcon } from '../icons';
import MiniConstellation from '../porthole/MiniConstellation';
import { appHealth, appURL } from '../../utils/workspace';
import { timeAgo } from '../../utils/time';

export default function AppCard({ app, deploy, pipeline }: { app: AppModel; deploy?: AppDeployRecord; pipeline?: Pipeline }) {
  const health = appHealth(app);
  const url = appURL(app.deployedURL);
  const target = app.deployTarget?.kind ?? 'Not configured';
  const deployPath = deploy?.pipelineId && deploy.runId
    ? '/apps/' + app.id + '/deployments/' + deploy.pipelineId + '/' + deploy.runId : undefined;
  return (
    <article className="workspace-app-card">
      <div className="workspace-card-top">
        <div className="workspace-app-icon" aria-hidden="true">
          {pipeline ? <MiniConstellation stages={pipeline.stages ?? []} edges={pipeline.edges ?? []} /> : <AppsIcon width={24} height={24} />}
        </div>
        <Badge variant={health.variant}>{health.label}</Badge>
      </div>
      <h3><Link to={'/apps/' + app.id}>{app.name}</Link></h3>
      <p>{app.description || 'Review configuration and deployment history for this app.'}</p>
      <div className="workspace-repo">{app.githubRepo || 'Source not configured'}<br />{app.branch || 'Revision not configured'}</div>
      <div className="workspace-card-foot"><span>{target}</span><Link to={'/apps/' + app.id}>Review automation</Link></div>
      {app.healthCheckedAt ? <small>Health checked {timeAgo(app.healthCheckedAt)}</small> : null}
      <div className="workspace-card-links">
        <Link to={'/apps/' + app.id}>Manage app →</Link>
        {deployPath ? <Link to={deployPath}>Latest deployment</Link> : null}
        {pipeline ? <Link to={'/pipelines/' + pipeline.id + '/edit'}>View DAG</Link> : null}
        {url ? <a href={url} target="_blank" rel="noreferrer">Open app ↗</a> : null}
      </div>
    </article>
  );
}
