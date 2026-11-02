import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type { AppDeployRecord, AppDriftReport, AppModel } from '../types/app';
import Badge from '../components/ui/Badge';
import Caps from '../components/ui/Caps';
import Panel from '../components/ui/Panel';
import AppServices from '../components/catalog/ServiceGroups';
import AutomationPanel from '../components/app/AutomationPanel';
import AppEnvironmentBindings from '../components/app/AppEnvironmentBindings';
import AppRegistryAccess from '../components/app/AppRegistryAccess';
import { useAuth } from '../auth/OIDCProvider';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions } from '../components/ui/form';
import { ChartRows, type ChartRow, type ChartStatus } from '../components/list/StarChart';
import { usePortholeTransition } from '../hooks/usePortholeTransition';
import { pushToast } from '../stores/toastStore';
import { shortId, timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

function deployStatus(s: string): ChartStatus {
  if (s === 'success') return 'ok';
  if (s === 'failed') return 'fail';
  if (s === 'running' || s === 'pending') return 'running';
  return 'idle';
}

export default function AppDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const open = usePortholeTransition();
  const { user } = useAuth();
  const canEdit = !!user?.roles.some((role) => role === 'admin' || role === 'operator');
  const isAdmin = !!user?.roles.includes('admin');
  const [app, setApp] = useState<AppModel | null>(null);
  const [history, setHistory] = useState<AppDeployRecord[]>([]);
  const [historyUnavailable, setHistoryUnavailable] = useState(false);
  const refreshVersion = useRef(0);
  const [drift, setDrift] = useState<AppDriftReport | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [deploying, setDeploying] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!id) return;
    const version = ++refreshVersion.current;
    try {
      const [a, h, d] = await Promise.all([
        appsApi.get(id).catch(() => null),
        appsApi.listDeploys(id).then((r) => r?.deploys ?? []).catch(() => null),
        appsApi.drift(id).catch(() => null),
      ]);
      if (version !== refreshVersion.current) return;
      setHistoryUnavailable(h === null);
      if (h !== null) setHistory(h);
      else setHistory((previous) => previous.filter((row) => row.appId === id));
      setDrift(d);
      if (a === null) setError('App details are unavailable. Try refreshing.');
      else {
        setApp(a);
        setError(null);
      }
    } catch {
      if (version !== refreshVersion.current) return;
      setError('App details are unavailable. Try refreshing.');
    }
  }, [id]);

  const invalidateRefresh = useCallback(() => { refreshVersion.current++; }, []);

  useEffect(() => {
    setApp(null);
    setHistory([]);
    setHistoryUnavailable(false);
    void refresh();
    return invalidateRefresh;
  }, [refresh, invalidateRefresh]);
  // Poll faster while a canary is in flight.
  const canaryLive = app?.activeCanary?.status === 'progressing';
  useEffect(() => {
    const t = window.setInterval(() => void refresh(), canaryLive ? 5000 : 15000);
    return () => window.clearInterval(t);
  }, [refresh, canaryLive]);

  const deploy = async () => {
    setDeploying(true);
    try {
      const res = await appsApi.deploy(id);
      pushToast('success', `Deploy ${shortId(res.runId)} started.`);
    if (res.strategy !== 'canary' && res.deploymentView && res.pipelineId) { open(res.deploymentView, null); return; }
      // The deploy record (with its pipeline id) lands a moment later; follow it into the porthole.
      for (let i = 0; i < 8; i++) {
        await new Promise((r) => window.setTimeout(r, 1500));
        const recs = await appsApi.listDeploys(id, 5).then((r) => r?.deploys ?? []).catch(() => []);
        const rec = recs.find((d) => d.runId === res.runId);
        if (rec?.pipelineId) {
          open(`/apps/${id}/deployments/${rec.pipelineId}/${rec.runId}`, null);
          return;
        }
      }
      await refresh();
    } catch (e) {
      pushToast('error', message(e));
    } finally {
      setDeploying(false);
    }
  };

  const rollback = async (deployId: string) => {
    setBusy(`rollback:${deployId}`);
    try {
      await appsApi.rollback(id, deployId);
      pushToast('success', 'Rollback started.');
      window.setTimeout(() => void refresh(), 3000);
    } catch (e) {
      pushToast('error', message(e));
    } finally {
      setBusy(null);
    }
  };

  const setStrategy = async (strategy: 'rolling' | 'canary') => {
    if (!app) return;
    setBusy('strategy');
    try {
      await appsApi.update(id, { ...app, canary: { ...(app.canary ?? { strategy: 'rolling' }), strategy } });
      await refresh();
    } catch (e) {
      pushToast('error', message(e));
    } finally {
      setBusy(null);
    }
  };

  const canaryAction = async (kind: 'promote' | 'abort') => {
    setBusy(kind);
    try {
      if (kind === 'promote') await appsApi.promoteCanary(id);
      else await appsApi.abortCanary(id);
      pushToast('success', kind === 'promote' ? 'Canary promoted.' : 'Canary aborted.');
      await refresh();
    } catch (e) {
      pushToast('error', message(e));
    } finally {
      setBusy(null);
    }
  };

  const remove = async () => {
    try {
      await appsApi.delete(id);
      pushToast('info', 'App deleted.');
      navigate('/apps');
    } catch (e) {
      pushToast('error', message(e));
    }
  };

  if (error && !app) {
    return (
      <div className="detail">
        <div className="form-error" role="alert">
          {error}
        </div>
        <Link className="hud-btn hud-link" to="/apps" style={{ alignSelf: 'flex-start' }}>
          Back to apps
        </Link>
      </div>
    );
  }
  if (!app) return <div className="detail" />;

  const health = app.healthStatus ?? 'unknown';
  const rows: ChartRow[] = history.map((d, i) => {
    const portholeUrl = d.pipelineId ? `/apps/${id}/deployments/${d.pipelineId}/${d.runId}` : null;
    return {
      id: d.id,
      name: d.imageRef ? d.imageRef.split('/').pop() ?? d.imageRef : `run ${shortId(d.runId)}`,
      sub: d.digest ? d.digest.slice(0, 19) : undefined,
      status: deployStatus(d.status),
      meta: [d.kind === 'rejected' ? 'Rejected before deployment' : d.kind, d.status, timeAgo(d.createdAt)],
      trailing: (
        <>
          {portholeUrl && (
            <a
              href={portholeUrl}
              onClick={(ev) => {
                ev.preventDefault();
                open(portholeUrl, null);
              }}
            >
              porthole ↗
            </a>
          )}
          {canEdit && i > 0 && d.status === 'success' && d.kind === 'deploy' && d.imageRef && (
            <ConfirmButton className="hud-btn" confirmLabel="Roll back?" onConfirm={() => rollback(d.id)} disabled={busy !== null}>
              Roll back
            </ConfirmButton>
          )}
        </>
      ),
    };
  });

  return (
    <div className="detail">
      {error && <div className="form-error" role="alert">{error}</div>}
      <header className="detail-head">
        <div className="grow">
          <h1>{app.name}</h1>
          {app.description && <p>{app.description}</p>}
          <div className="detail-badges">
            <Badge variant={health === 'healthy' ? 'ok' : health === 'failed' ? 'fail' : health === 'degraded' ? 'running' : 'muted'}>{health}</Badge>
            {app.hasWebhook && <Badge variant="muted">webhook</Badge>}
            {app.canary?.strategy === 'canary' && <Badge variant="ember">canary</Badge>}
            {drift?.status === 'in_sync' && <Badge variant="ok">in sync</Badge>}
            {drift?.status === 'drift' && <Badge variant="fail">drift</Badge>}
          </div>
          <div className="detail-meta">
            <span>
              {app.githubRepo}@{app.branch}
            </span>
            <span>{app.deployTarget?.kind}</span>
            {app.buildPlan?.commit && <span>commit {app.buildPlan.commit.slice(0, 12)}</span>}
            {app.deployTarget?.prefix && <span>prefix {app.deployTarget.prefix}</span>}
            {app.environmentId && <span>env {shortId(app.environmentId)}</span>}
            <span>Manual deploy · optional Git rules</span>
            {app.deployedURL && (
              <a href={app.deployedURL} target="_blank" rel="noreferrer">
                Open app ↗
              </a>
            )}
          </div>
        </div>
        <Actions>
          {canEdit && app.buildPlan?.kind === 'compose' && <Link className="hud-btn" to={`/apps/new?appId=${encodeURIComponent(app.id)}`}>Review source & targets</Link>}
          {canEdit && <button type="button" className="hud-btn hud-btn-primary" onClick={deploy} disabled={deploying}>
            {deploying ? 'Deploying…' : '▶ Deploy'}
          </button>}
          {isAdmin && <ConfirmButton className="hud-btn" confirmLabel="Delete app?" onConfirm={remove}>
            Delete
          </ConfirmButton>}
        </Actions>
      </header>

      <div className="panel-grid">
        <AppServices nativeAppId={app.id} />
        <Panel title="Deploy history" aside={historyUnavailable ? 'Unavailable' : `${history.length} ${history.length === 1 ? 'attempt' : 'attempts'}`} className="panel-span">
          <button className="hud-btn" onClick={() => void refresh()}>Refresh history</button>
          {historyUnavailable && <p role="alert">Deployment history is unavailable.{rows.length ? ' Showing the last loaded attempts.' : ' Try refreshing.'}</p>}
          {rows.length ? <ChartRows rows={rows} hasThumbs={false} /> : !historyUnavailable && <p>No deploys yet. Press Deploy to build, push and roll out {app.branch}.</p>}
        </Panel>

        <Panel title="Git deployment rules" className="panel-span"><AutomationPanel app={app} onChanged={refresh} /></Panel>
        <Panel title="Configuration & protected key bindings" className="panel-span"><AppEnvironmentBindings key={app.id} app={app} onSaved={refresh} /></Panel>
        <Panel title="Registry access" className="panel-span"><AppRegistryAccess key={app.id} app={app} onSaved={refresh} /></Panel>

        <Panel title="Drift" aside={drift?.status ?? 'unknown'}>
          {drift ? (
            <div className="kv">
              <Caps>Expected</Caps>
              <span className={drift.expectedImage ? 'v' : 'v muted'}>{drift.expectedImage ?? '—'}</span>
              <Caps>Live</Caps>
              <span className={drift.liveImage ? 'v' : 'v muted'}>{drift.liveImage ?? '—'}</span>
              {drift.message && (
                <>
                  <Caps>Note</Caps>
                  <span className="v muted">{drift.message}</span>
                </>
              )}
            </div>
          ) : (
            <p>Drift is reported after the first deploy.</p>
          )}
          {app.healthMessage && (
            <div className="kv">
              <Caps>Health</Caps>
              <span className="v muted">{app.healthMessage}</span>
            </div>
          )}
        </Panel>

        <Panel title="Rollout" aside={app.canary?.strategy ?? 'rolling'}>
          <div className="option-grid" role="group" aria-label="Strategy">
            <button type="button" className="option" aria-pressed={(app.canary?.strategy ?? 'rolling') === 'rolling'} onClick={() => setStrategy('rolling')} disabled={!canEdit || busy !== null}>
              <Caps>Rolling</Caps>
              <small>Replace the running version</small>
            </button>
            <button type="button" className="option" aria-pressed={app.canary?.strategy === 'canary'} onClick={() => setStrategy('canary')} disabled={!canEdit || busy !== null}>
              <Caps>Canary</Caps>
              <small>Shift traffic, then promote</small>
            </button>
          </div>
          {app.activeCanary ? (
            <>
              <div className="kv">
                <Caps>Status</Caps>
                <span className="v">
                  {app.activeCanary.status} · {app.activeCanary.weight}% · {app.activeCanary.healthy ? 'healthy' : 'unhealthy'}
                </span>
                <Caps>Canary</Caps>
                <span className="v">{app.activeCanary.canaryImage}</span>
                {app.activeCanary.stableImage && (
                  <>
                    <Caps>Stable</Caps>
                    <span className="v">{app.activeCanary.stableImage}</span>
                  </>
                )}
              </div>
              {canEdit && app.activeCanary.status === 'progressing' && (
                <Actions>
                  <ConfirmButton className="hud-btn hud-btn-primary" confirmLabel="Promote to 100%?" onConfirm={() => canaryAction('promote')} disabled={busy !== null}>
                    Promote
                  </ConfirmButton>
                  <ConfirmButton className="hud-btn" confirmLabel="Abort and roll back?" onConfirm={() => canaryAction('abort')} disabled={busy !== null}>
                    Abort
                  </ConfirmButton>
                </Actions>
              )}
            </>
          ) : (
            <p>No canary in flight.</p>
          )}
        </Panel>
      </div>
    </div>
  );
}
