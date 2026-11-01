import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { appsApi } from '../../api/apps';
import type { AppModel } from '../../types/app';

/** Current reviewed stack revisions stored by Cooker, shared across browsers. */
export default function GitHubStacks({ query }: { query: string }) {
  const [apps, setApps] = useState<AppModel[]>([]);
  const [offset, setOffset] = useState(0);
  const [more, setMore] = useState(false);
  const [error, setError] = useState(false);
  useEffect(() => {
    let alive = true;
    void appsApi.list({ limit: 100, offset }).then((items) => {
      if (!alive) return;
      setApps((previous) => offset ? [...previous, ...(items ?? [])] : items ?? []);
      setMore(items?.length === 100); setError(false);
    }).catch(() => { if (alive) setError(true); });
    return () => { alive = false; };
  }, [offset]);
  const stacks = apps.filter((app) => app.buildPlan?.kind === 'compose' && `${app.name} ${app.githubRepo} ${app.deployTarget.prefix}`.toLowerCase().includes(query.trim().toLowerCase()));
  return <section className="compose-shared-stacks" aria-label="GitHub stacks">
    <div><h3>GitHub stacks</h3><span className="compose-muted">Saved in Cooker · shared across browsers</span></div>
    <Link to="/apps/new" className="hud-btn hud-btn-primary">Import from GitHub</Link>
    {error && <p className="compose-muted">Could not load saved apps. <Link to="/apps">Open Apps</Link> to retry.</p>}
    {!error && stacks.length === 0 && <p className="compose-muted">{query ? 'No matching repository stacks.' : 'Imported stacks will appear here after you save a reviewed revision.'}</p>}
    <div className="compose-shared-list">{stacks.map((app) => <Link key={app.id} to={`/apps/${app.id}`} className="compose-shared-stack" aria-label={`Open saved app ${app.name}`}>
      <strong>{app.name}</strong><span>{app.githubRepo}</span><code>{app.buildPlan?.commit?.slice(0, 12) || app.branch} · {app.deployTarget.prefix || app.name}</code>
    </Link>)}</div>
    {more && <button type="button" className="hud-btn" onClick={() => setOffset(offset + 100)}>Load more apps</button>}
  </section>;
}
