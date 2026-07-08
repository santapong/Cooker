import { useState, useEffect, useRef, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type {
  AppModel,
  AppDeployResponse,
  AppDeployRecord,
  AppDriftReport,
  CanaryConfig,
} from '../types/app';
import { useTheme } from '../theme/ThemeProvider';
import { Btn, PageHeader } from '../components/ui/atoms';
import { useToastStore } from '../stores/toastStore';
import { useWebSocket } from '../hooks/useWebSocket';
import HealthBadge from './appdetail/HealthBadge';
import OverviewPanel from './appdetail/OverviewPanel';
import DeploymentsPanel from './appdetail/DeploymentsPanel';
import DeployLogPanel from './appdetail/DeployLogPanel';
import ServicesPanel from './appdetail/ServicesPanel';

export default function AppDetailPage() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [app, setApp] = useState<AppModel | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [deploying, setDeploying] = useState(false);
  const [lastDeploy, setLastDeploy] = useState<AppDeployResponse | null>(null);
  const [history, setHistory] = useState<AppDeployRecord[]>([]);
  const [drift, setDrift] = useState<AppDriftReport | null>(null);
  const [logs, setLogs] = useState<string>('');
  // streamRunId drives useWebSocket — null means "no active stream".
  const [streamRunId, setStreamRunId] = useState<string | null>(null);
  const logRef = useRef<HTMLPreElement | null>(null);
  // Holds the post-rollback history-refresh timer so it can be cleared
  // on unmount and avoid setState-after-unmount (FE-M AppDetailPage).
  const rollbackTimerRef = useRef<number | null>(null);

  // Webhook rotation panel state.
  const [showWebhook, setShowWebhook] = useState(false);
  const [newSecret, setNewSecret] = useState('');
  const [rotating, setRotating] = useState(false);

  // Canary config form + live rollout state (OR-1). The form draft is
  // seeded from the app once it loads and saved via appsApi.update.
  const [canaryDraft, setCanaryDraft] = useState<CanaryConfig | null>(null);
  const [savingCanary, setSavingCanary] = useState(false);
  const [canaryBusy, setCanaryBusy] = useState(false);
  const activeCanary = app?.activeCanary ?? null;

  // Webhook URL (Indie step 5, PR #50 finding W11-A1).
  // Derived from window.location.origin — no new API field needed.
  const webhookUrl = `${window.location.origin}/api/v1/webhooks/github`;

  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    const refetch = () => {
      appsApi
        .get(id)
        .then((next) => {
          if (cancelled) return;
          setApp(next);
          // Seed the canary form once; later polls must not clobber an
          // in-progress edit, so only fill it when still empty.
          setCanaryDraft((d) => d ?? next.canary ?? { strategy: 'rolling' });
        })
        .catch((e) => {
          if (!cancelled) setError((e as Error).message);
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
    };
    refetch();
    // Refresh every 30s so the post-deploy health badge moves
    // unknown -> healthy / degraded / failed as the backend
    // AppHealthChecker writes new verdicts. Cheap: one GET / 30s
    // per open AppDetailPage instance.
    const tick = window.setInterval(refetch, 30_000);
    return () => {
      cancelled = true;
      window.clearInterval(tick);
    };
  }, [id]);

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [logs]);

  // Clear the post-rollback refresh timer on unmount so setState is never
  // called after the component is gone (FE-M AppDetailPage).
  useEffect(() => {
    return () => {
      if (rollbackTimerRef.current !== null) {
        window.clearTimeout(rollbackTimerRef.current);
      }
    };
  }, []);

  // Migrate away from raw new WebSocket() (PR #50 finding B3.2, FH-03 fix PR #49).
  // useWebSocket handles the 60s ticket flow, reconnect-with-backoff, and
  // the FH-03 race-condition guard so we do not duplicate that logic here.
  const wsUrl = streamRunId ? `/ws/app-run/${encodeURIComponent(streamRunId)}` : '';

  const onWsMessage = useCallback((data: unknown) => {
    const raw = typeof data === 'string' ? data : JSON.stringify(data);
    setLogs((s) => s + raw);
  }, []);

  const { connected: wsConnected } = useWebSocket({
    url: wsUrl,
    autoConnect: !!wsUrl,
    // No reconnect for log streaming: the backend closes the channel when
    // the run finishes. Reconnect would re-subscribe to an already-closed
    // channel and produce misleading "streaming" UI state.
    reconnect: { enabled: false },
    onMessage: onWsMessage,
  });

  // When the WebSocket closes (wsConnected flips false after having been
  // true), the deploy run is done — clear the deploying spinner.
  const prevConnected = useRef(false);
  useEffect(() => {
    if (prevConnected.current && !wsConnected) {
      setDeploying(false);
    }
    prevConnected.current = wsConnected;
  }, [wsConnected]);

  const deploy = async () => {
    if (!id) return;
    setDeploying(true);
    setLogs('');
    setStreamRunId(null);
    try {
      const res = await appsApi.deploy(id);
      setLastDeploy(res);
      // Trigger the useWebSocket hook by setting the run ID.
      setStreamRunId(res.runId);
    } catch (e) {
      setError((e as Error).message);
      setDeploying(false);
    }
  };

  const refreshHistory = useCallback(() => {
    if (!id) return;
    appsApi
      .listDeploys(id)
      .then((res) => setHistory(res.deploys))
      .catch(() => {
        /* history is additive UI; stay quiet */
      });
    appsApi
      .drift(id)
      .then(setDrift)
      .catch(() => setDrift(null));
  }, [id]);

  useEffect(() => {
    refreshHistory();
  }, [refreshHistory]);

  // While a canary is progressing, poll the app faster (5s) so the panel
  // reflects auto-promote / rollback / weight changes promptly. The 30s
  // refetch above still runs; this just tightens the loop when it matters.
  const refetchApp = useCallback(() => {
    if (!id) return;
    appsApi
      .get(id)
      .then(setApp)
      .catch(() => {
        /* additive refresh; stay quiet */
      });
  }, [id]);

  useEffect(() => {
    if (activeCanary?.status !== 'progressing') return;
    const tick = window.setInterval(refetchApp, 5_000);
    return () => window.clearInterval(tick);
  }, [activeCanary?.status, refetchApp]);

  const saveCanary = async () => {
    if (!id || !app || !canaryDraft) return;
    setSavingCanary(true);
    try {
      // Send the full app with the edited canary config. The backend
      // normalises + validates (weight 1-99); a 4xx surfaces as a toast.
      await appsApi.update(id, { ...app, canary: canaryDraft });
      pushToast({ kind: 'success', message: 'Deploy strategy saved.' });
      refetchApp();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setSavingCanary(false);
    }
  };

  const promoteCanary = async () => {
    if (!id) return;
    if (!confirm('Promote the canary to 100% of traffic?')) return;
    setCanaryBusy(true);
    try {
      await appsApi.promoteCanary(id);
      pushToast({ kind: 'success', message: 'Canary promoted to 100%.' });
      refetchApp();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setCanaryBusy(false);
    }
  };

  const abortCanary = async () => {
    if (!id) return;
    if (!confirm('Abort the canary and roll back to the stable version?')) return;
    setCanaryBusy(true);
    try {
      await appsApi.abortCanary(id);
      pushToast({ kind: 'success', message: 'Canary aborted; rolled back to stable.' });
      refetchApp();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setCanaryBusy(false);
    }
  };

  const rollback = async (deployId: string, imageRef: string) => {
    if (!id) return;
    if (!confirm(`Roll back to ${imageRef}? This re-deploys that image.`)) return;
    setLogs('');
    setStreamRunId(null);
    try {
      const res = await appsApi.rollback(id, deployId);
      pushToast({ kind: 'success', message: `Rolling back to ${res.rolledBackTo.imageRef}.` });
      setStreamRunId(res.runId);
      if (rollbackTimerRef.current !== null) window.clearTimeout(rollbackTimerRef.current);
      rollbackTimerRef.current = window.setTimeout(refreshHistory, 4000);
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  const remove = async () => {
    if (!id) return;
    if (!confirm('Delete this app?')) return;
    await appsApi.delete(id);
    navigate('/apps');
  };

  const rotateWebhook = async () => {
    if (!id || !newSecret) return;
    setRotating(true);
    try {
      await appsApi.setWebhookSecret(id, newSecret);
      pushToast({ kind: 'success', message: 'Webhook secret rotated.' });
      setNewSecret('');
      setShowWebhook(false);
      // Refetch so the hasWebhook badge reflects the new state.
      const updated = await appsApi.get(id);
      setApp(updated);
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setRotating(false);
    }
  };

  const generateSecret = () => {
    const arr = new Uint8Array(24);
    window.crypto.getRandomValues(arr);
    setNewSecret(
      Array.from(arr)
        .map((b) => b.toString(16).padStart(2, '0'))
        .join(''),
    );
  };

  const copyWebhookUrl = () => {
    navigator.clipboard.writeText(webhookUrl).then(() => {
      pushToast({ kind: 'success', message: 'Webhook URL copied' });
    }).catch(() => {
      pushToast({ kind: 'error', message: 'Could not copy to clipboard' });
    });
  };

  const openWebhookForm = () => setShowWebhook(true);
  const cancelWebhookForm = () => {
    setShowWebhook(false);
    setNewSecret('');
  };

  if (loading) {
    return (
      <div
        style={{
          padding: 60,
          color: t.textMute,
          fontFamily: t.serif,
          fontSize: 18,
          textAlign: 'center',
        }}
      >
        Loading…
      </div>
    );
  }
  if (error || !app) {
    return (
      <div style={{ padding: 60, color: t.bad, textAlign: 'center' }}>
        {error ?? 'App not found'}
      </div>
    );
  }

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow={`github.com/${app.githubRepo} @ ${app.branch}`}
        title={app.name}
        subtitle={
          <>
            target: <strong style={{ color: t.text }}>{app.deployTarget.kind}</strong>
            {app.deployTarget.namespace ? ` · ns/${app.deployTarget.namespace}` : ''}
            <HealthBadge app={app} />
          </>
        }
        actions={
          <>
            <Btn kind="danger" onClick={remove}>Delete</Btn>
            <Btn kind="primary" icon="play" onClick={deploy} disabled={deploying}>
              {deploying ? 'Deploying…' : 'Deploy'}
            </Btn>
          </>
        }
      />

      {/* 320px sidebar (facts + deploy activity + service config) / 1fr log
          viewer. OverviewPanel, DeploymentsPanel and ServicesPanel are called
          in this order so the rendered card sequence matches the original
          single-file layout exactly (see each panel's file comment for why
          the canary-rollout card lives in DeploymentsPanel rather than
          ServicesPanel). DeployLogPanel is a separate grid column, not a
          sidebar card, so it has its own call site below. */}
      <div style={{ display: 'grid', gridTemplateColumns: '320px 1fr', gap: 22 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
          <OverviewPanel app={app} drift={drift} />

          <DeploymentsPanel
            activeCanary={activeCanary}
            canaryBusy={canaryBusy}
            onPromoteCanary={promoteCanary}
            onAbortCanary={abortCanary}
            history={history}
            onRollback={rollback}
          />

          <ServicesPanel
            app={app}
            webhookUrl={webhookUrl}
            showWebhook={showWebhook}
            newSecret={newSecret}
            rotating={rotating}
            onShowWebhook={openWebhookForm}
            onCancelWebhook={cancelWebhookForm}
            onChangeSecret={setNewSecret}
            onGenerateSecret={generateSecret}
            onCopyWebhookUrl={copyWebhookUrl}
            onRotateWebhook={rotateWebhook}
            canaryDraft={canaryDraft}
            onChangeCanaryDraft={setCanaryDraft}
            savingCanary={savingCanary}
            onSaveCanary={saveCanary}
          />
        </div>

        <DeployLogPanel logs={logs} logRef={logRef} lastDeploy={lastDeploy} deploying={deploying} />
      </div>
    </div>
  );
}
