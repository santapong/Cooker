import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type { AppDeployRecord, AppDriftReport, AppModel } from '../types/app';
import Badge from '../components/ui/Badge';
import Caps from '../components/ui/Caps';
import Panel from '../components/ui/Panel';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Field, TextInput } from '../components/ui/form';
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
  const [app, setApp] = useState<AppModel | null>(null);
  const [history, setHistory] = useState<AppDeployRecord[]>([]);
  const [drift, setDrift] = useState<AppDriftReport | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [deploying, setDeploying] = useState(false);
  const [secret, setSecret] = useState('');
  const [busy, setBusy] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!id) return;
    try {
      const [a, h, d] = await Promise.all([
        appsApi.get(id),
        appsApi.listDeploys(id).then((r) => r?.deploys ?? []).catch(() => [] as AppDeployRecord[]),
        appsApi.drift(id).catch(() => null),
      ]);
      setApp(a);
      setHistory(h);
      setDrift(d);
      setError(null);
    } catch (e) {
      setError(message(e));
    }
  }, [id]);

  useEffect(() => {
    void refresh();
  }, [refresh]);
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

  const saveSecret = async () => {
    if (!secret.trim()) return;
    setBusy('secret');
    try {
      await appsApi.setWebhookSecret(id, secret.trim());
      pushToast('success', 'Webhook secret saved.');
      setSecret('');
      await refresh();
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

  const webhookUrl = `${window.location.origin}/api/v1/webhooks/github`;
  const health = app.healthStatus ?? 'unknown';
  const rows: ChartRow[] = history.map((d, i) => {
    const portholeUrl = d.pipelineId ? `/apps/${id}/deployments/${d.pipelineId}/${d.runId}` : null;
    return {
      id: d.id,
      name: d.imageRef ? d.imageRef.split('/').pop() ?? d.imageRef : `run ${shortId(d.runId)}`,
      sub: d.digest ? d.digest.slice(0, 19) : undefined,
      status: deployStatus(d.status),
      meta: [d.kind, d.status, timeAgo(d.createdAt)],
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
          {i > 0 && d.status === 'success' && d.kind === 'deploy' && d.imageRef && (
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
            {app.environmentId && <span>env {shortId(app.environmentId)}</span>}
            <span>{app.autoDeploy ? 'auto-deploy' : 'manual deploy'}</span>
            {app.deployedURL && (
              <a href={app.deployedURL} target="_blank" rel="noreferrer">
                Open app ↗
              </a>
            )}
          </div>
        </div>
        <Actions>
          <button type="button" className="hud-btn hud-btn-primary" onClick={deploy} disabled={deploying}>
            {deploying ? 'Deploying…' : '▶ Deploy'}
          </button>
          <ConfirmButton className="hud-btn" confirmLabel="Delete app?" onConfirm={remove}>
            Delete
          </ConfirmButton>
        </Actions>
      </header>

      <div className="panel-grid">
        <Panel title="Deploy history" aside={`${history.length} ${history.length === 1 ? 'deploy' : 'deploys'}`} className="panel-span">
          {rows.length ? <ChartRows rows={rows} hasThumbs={false} /> : <p>No deploys yet. Press Deploy to build, push and roll out {app.branch}.</p>}
        </Panel>

        <Panel title="Webhook" aside={app.hasWebhook ? 'configured' : 'not configured'}>
          <p>Point a GitHub push webhook at this URL; auto-deploy uses it.</p>
          <code className="code">{webhookUrl}</code>
          <Field label="Secret">
            <TextInput type="password" autoComplete="off" value={secret} placeholder="shared secret" onChange={(e) => setSecret(e.target.value)} />
          </Field>
          <Actions>
            <button type="button" className="hud-btn" onClick={saveSecret} disabled={!secret.trim() || busy !== null}>
              {busy === 'secret' ? 'Saving…' : 'Save secret'}
            </button>
          </Actions>
        </Panel>

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
            <button type="button" className="option" aria-pressed={(app.canary?.strategy ?? 'rolling') === 'rolling'} onClick={() => setStrategy('rolling')} disabled={busy !== null}>
              <Caps>Rolling</Caps>
              <small>Replace the running version</small>
            </button>
            <button type="button" className="option" aria-pressed={app.canary?.strategy === 'canary'} onClick={() => setStrategy('canary')} disabled={busy !== null}>
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
              {app.activeCanary.status === 'progressing' && (
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
