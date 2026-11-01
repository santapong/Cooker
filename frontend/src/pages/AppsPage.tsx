import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { appsApi } from '../api/apps';
import { pipelineApi } from '../api/pipelines';
import type { AppDeployRecord, AppModel } from '../types/app';
import type { Pipeline } from '../types/pipeline';
import StarChart, { type ChartRow, type ChartStatus } from '../components/list/StarChart';
import MiniConstellation from '../components/porthole/MiniConstellation';
import { AppsIcon } from '../components/icons';
import { usePortholeTransition } from '../hooks/usePortholeTransition';
import { shortId, timeAgo } from '../utils/time';

const prefetchDeployment = () => void import('./DeploymentPage');

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

function healthStatus(app: AppModel): ChartStatus {
  switch (app.healthStatus) {
    case 'healthy':
      return 'ok';
    case 'failed':
      return 'fail';
    case 'degraded':
      return 'warn';
    default:
      return app.activeCanary ? 'running' : 'idle';
  }
}

export default function AppsPage() {
  const open = usePortholeTransition();
  const [apps, setApps] = useState<AppModel[]>([]);
  const [deploys, setDeploys] = useState<Record<string, AppDeployRecord>>({});
  const [pipelines, setPipelines] = useState<Record<string, Pipeline>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    appsApi
      .list({ limit: 100 })
      .then(async (list) => {
        if (cancelled) return;
        setApps(list ?? []);
        setLoading(false);
        // Latest deploy per app → its pipeline draws the thumbnail and links the deployment view.
        const latest = await Promise.all(
          (list ?? []).map(async (a) => {
            try {
              const d = await appsApi.listDeploys(a.id, 1);
              return [a.id, d?.deploys?.[0]] as const;
            } catch {
              return [a.id, undefined] as const;
            }
          }),
        );
        if (cancelled) return;
        const byApp: Record<string, AppDeployRecord> = {};
        for (const [id, d] of latest) if (d) byApp[id] = d;
        setDeploys(byApp);
        const ids = Array.from(new Set(Object.values(byApp).map((d) => d.pipelineId).filter((id): id is string => !!id)));
        const found = await Promise.all(
          ids.map(async (id) => {
            try {
              return await pipelineApi.get(id);
            } catch {
              return null;
            }
          }),
        );
        if (cancelled) return;
        const next: Record<string, Pipeline> = {};
        for (const p of found) if (p) next[p.id] = p;
        setPipelines(next);
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          setError(message(e));
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const rows: ChartRow[] = apps.map((app) => {
    const deploy = deploys[app.id];
    const p = deploy?.pipelineId ? pipelines[deploy.pipelineId] : undefined;
    const href = `/apps/${app.id}`;
    const target = app.deployTarget?.kind ?? 'target';
    const deployUrl = deploy?.pipelineId && deploy.runId ? `/apps/${app.id}/deployments/${deploy.pipelineId}/${deploy.runId}` : null;
    return {
      id: app.id,
      name: app.name,
      sub: app.description || `${app.githubRepo}@${app.branch}`,
      status: healthStatus(app),
      thumb: p ? <MiniConstellation stages={p.stages ?? []} edges={p.edges ?? []} /> : <AppsIcon width={22} height={22} />,
      href,
      onOpen: (el) => open(href, el),
      meta: [
        target,
        app.healthStatus ? `${app.healthStatus}${app.healthCheckedAt ? ` ${timeAgo(app.healthCheckedAt)}` : ''}` : 'not deployed',
        app.autoDeploy ? 'auto-deploy' : 'manual',
      ],
      trailing: (
        <>
          {deployUrl && (
            <a
              href={deployUrl}
              onMouseEnter={prefetchDeployment}
              onClick={(ev) => {
                ev.preventDefault();
                ev.stopPropagation();
                open(deployUrl, null);
              }}
            >
              deploy {shortId(deploy?.runId)} ↗
            </a>
          )}
          {app.deployedURL && (
            <a href={app.deployedURL} target="_blank" rel="noreferrer" onClick={(ev) => ev.stopPropagation()}>
              Open app ↗
            </a>
          )}
        </>
      ),
    };
  });

  return (
    <StarChart
      title="Apps"
      count={apps.length}
      rows={rows}
      loading={loading}
      error={error}
      empty={{
        text: 'No apps yet. Point Cooker at a repository and it will build, push and deploy it.',
        action: (
          <Link className="hud-btn hud-btn-primary hud-link" to="/apps/new">
            ＋ New app
          </Link>
        ),
      }}
      actions={
        <Link className="hud-btn hud-btn-primary hud-link" to="/apps/new">
          ＋ New app
        </Link>
      }
    />
  );
}
