import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { vercelApi } from '../../api/vercel';
import { useAuth } from '../../auth/OIDCProvider';
import type { VercelSnapshot } from '../../types/vercel';
import { timeAgo } from '../../utils/time';
import { vercelDeploymentStatus, vercelDeploymentURL, vercelSyncStatus } from '../../utils/vercel';
import Badge from '../ui/Badge';
import Panel from '../ui/Panel';
import { Actions, Field, FormError, Select } from '../ui/form';
import VercelLandingPublish from './VercelLandingPublish';
import VercelCoverage from './VercelCoverage';
import IntegrationAccess from '../catalog/IntegrationAccess';
import './vercel.css';

export default function VercelDeployments() {
  const admin = useAuth().user?.roles.includes('admin');
  const [snapshot, setSnapshot] = useState<VercelSnapshot | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState('');
  const [connectionFilter, setConnectionFilter] = useState('');
  const [projectFilter, setProjectFilter] = useState('');
  const request = useRef(0);
  const mounted = useRef(false);
  const fetch = useCallback(async () => {
    const current = ++request.current;
    try {
      const result = await vercelApi.snapshot();
      if (mounted.current && current === request.current) { setSnapshot(result); setError(''); }
    } catch (e) {
      if (mounted.current && current === request.current) setError(e instanceof Error ? e.message : 'Could not refresh Vercel history.');
    } finally { if (mounted.current && current === request.current) setLoading(false); }
  }, []);
  useEffect(() => {
    mounted.current = true;
    void fetch();
    const timer = window.setInterval(() => void fetch(), 30_000);
    return () => { mounted.current = false; window.clearInterval(timer); };
  }, [fetch]);

  async function sync(id: string) {
    setBusy(id); setError('');
    try {
      const result = await vercelApi.sync(id);
      await fetch();
      if (mounted.current && result.connection.syncError) setError(result.connection.syncError);
    } catch (e) { if (mounted.current) setError(e instanceof Error ? e.message : 'Could not sync Vercel.'); }
    finally { if (mounted.current) setBusy(''); }
  }
  const connections = snapshot?.connections ?? [];
  const projects = (snapshot?.projects ?? []).filter((project) => !connectionFilter || project.connectionId === connectionFilter);
  const deployments = (snapshot?.deployments ?? []).filter((deployment) => (!connectionFilter || deployment.connectionId === connectionFilter) && (!projectFilter || `${deployment.connectionId}:${deployment.projectId}` === projectFilter));

  return <><VercelLandingPublish connections={connections} connectionsLoading={loading} /><Panel title="Vercel deployments" aside={<Link to="/settings#vercel-connections">{admin ? 'Manage connections' : 'Connection settings'}</Link>}>
    <p>Deployment status and recent history imported from your selected Vercel projects. Cooker landing publication receipts are shown separately above.</p>
    <FormError>{error ? `${error}${snapshot ? ' Showing the last retrieved snapshot.' : ''}` : ''}</FormError>
    {error && <button type="button" className="hud-btn" onClick={() => void fetch()}>Retry Vercel history</button>}
    {loading && <p role="status">Loading Vercel history…</p>}
    {!loading && !connections.length && !error && <div className="vercel-card vercel-stack"><strong>Bring your Vercel projects into view</strong><p>{admin ? 'Connect an account or team, then choose the projects you want to follow.' : 'Ask an administrator to connect Vercel and select projects.'}</p>{admin && <Link className="hud-btn hud-link" to="/settings#vercel-connections">Connect Vercel in settings</Link>}</div>}
    {connections.length > 0 && <>
      <div className="panel-grid">
        {connections.map((connection) => {
          const status = vercelSyncStatus(connection, snapshot?.syncIntervalSeconds);
          return <article key={connection.id} className="vercel-card vercel-stack" aria-label={connection.name}>
            <div className="vercel-heading"><h3>{connection.name}</h3><Badge variant={status.variant}>{status.label}</Badge></div>
            <p className="vercel-meta">{connection.identity.name} · {connection.identity.scope === 'team' ? 'Team' : 'Personal account'} · {connection.projectIds.length} selected projects</p>
            <p className="vercel-meta" title={connection.lastSuccessAt}>Last successful sync: {connection.lastSuccessAt ? timeAgo(connection.lastSuccessAt) : 'Never'}</p>
            <VercelCoverage connection={connection} />
            <IntegrationAccess inventory={connection.integrations} compact scope="connection" projects={(snapshot?.projects ?? []).filter((project) => project.connectionId === connection.id)} />
            {connection.retryAt && <p className="vercel-meta">Retry available {timeAgo(connection.retryAt)}</p>}
            {connection.syncError && <FormError>{connection.syncError} Previous results may be out of date.</FormError>}
            {!connection.projectIds.length && <p className="vercel-hint">Choose projects in settings to start importing history.</p>}
            {admin && <Actions><button type="button" className="hud-btn" disabled={!!busy || connection.syncing || !connection.enabled || !connection.hasToken || !connection.projectIds.length || Date.parse(connection.retryAt ?? '') > Date.now()} onClick={() => void sync(connection.id)}>{busy === connection.id || connection.syncing ? 'Syncing…' : 'Sync now'}</button></Actions>}
          </article>;
        })}
      </div>
      <p className="vercel-hint">Cooker checks Vercel every {snapshot?.syncIntervalSeconds ?? 120} seconds. This view refreshes every 30 seconds. Up to {snapshot?.historyLimit ?? 100} recent deployments per selected project are retained; this is not a complete archive.</p>
      <div className="vercel-filters">
        <Field label="Vercel account or team"><Select value={connectionFilter} onChange={(e) => { setConnectionFilter(e.target.value); setProjectFilter(''); }} options={[{ value: '', label: 'All connections' }, ...connections.map((connection) => ({ value: connection.id, label: connection.name }))]} /></Field>
        <Field label="Vercel project"><Select value={projectFilter} onChange={(e) => setProjectFilter(e.target.value)} options={[{ value: '', label: 'All selected projects' }, ...projects.map((project) => ({ value: `${project.connectionId}:${project.id}`, label: `${project.name}${connectionFilter ? '' : ` · ${connections.find((connection) => connection.id === project.connectionId)?.name ?? project.connectionId}`}` }))]} /></Field>
      </div>
      <ul className="vercel-history" aria-label="Vercel deployment history">
        {deployments.map((deployment) => {
          const status = vercelDeploymentStatus(deployment.state);
          const url = vercelDeploymentURL(deployment.url);
          const connection = connections.find((item) => item.id === deployment.connectionId);
          const project = projects.find((item) => item.id === deployment.projectId && item.connectionId === deployment.connectionId);
          return <li key={`${deployment.connectionId}:${deployment.id}`} className="vercel-card">
            <div className="vercel-heading"><h3>{project?.name ?? deployment.name}</h3><Badge variant={status.variant}>{status.label}</Badge></div>
            <div className="vercel-meta">{connection?.name ?? deployment.connectionId} · {deployment.target || 'preview'} · <time dateTime={deployment.createdAt} title={deployment.createdAt}>{timeAgo(deployment.createdAt)}</time></div>
            {(deployment.gitBranch || deployment.gitCommitSHA) && <div className="vercel-meta">{deployment.gitBranch}{deployment.gitBranch && deployment.gitCommitSHA ? ' · ' : ''}<code title={deployment.gitCommitSHA}>{deployment.gitCommitSHA?.slice(0, 12)}</code></div>}
            <div className="vercel-meta">Deployment <code>{deployment.id}</code></div>
            {url ? <a href={url} target="_blank" rel="noopener noreferrer">Open deployment ↗</a> : <span className="vercel-meta">Deployment link unavailable</span>}
          </li>;
        })}
      </ul>
      {!deployments.length && <p>{connections.some((connection) => connection.lastSuccessAt) ? 'No deployments match these filters.' : 'History will appear after a successful sync.'}</p>}
    </>}
  </Panel></>;
}
