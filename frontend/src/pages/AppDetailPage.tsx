import { useState, useEffect, useRef, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type { AppModel, AppDeployResponse, AppDeployRecord, AppDriftReport } from '../types/app';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn, Card, Field, Input, Label, PageHeader, Pill, SectionLabel, statusTone } from '../components/ui/atoms';
import { useToastStore } from '../stores/toastStore';
import { useWebSocket } from '../hooks/useWebSocket';

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

  // Webhook rotation panel state.
  const [showWebhook, setShowWebhook] = useState(false);
  const [newSecret, setNewSecret] = useState('');
  const [rotating, setRotating] = useState(false);

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
          if (!cancelled) setApp(next);
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

  const rollback = async (deployId: string, imageRef: string) => {
    if (!id) return;
    if (!confirm(`Roll back to ${imageRef}? This re-deploys that image.`)) return;
    setLogs('');
    setStreamRunId(null);
    try {
      const res = await appsApi.rollback(id, deployId);
      pushToast({ kind: 'success', message: `Rolling back to ${res.rolledBackTo.imageRef}.` });
      setStreamRunId(res.runId);
      window.setTimeout(refreshHistory, 4000);
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

      <div style={{ display: 'grid', gridTemplateColumns: '320px 1fr', gap: 22 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
          <Card style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            <Field label="App ID" mono={app.id} />
            <Field label="Repo" mono={`github.com/${app.githubRepo}`} />
            <Field label="Branch" mono={app.branch} />
            {app.registryRef && <Field label="Registry ref" mono={app.registryRef} />}
            {app.environmentId && <Field label="Environment" mono={app.environmentId} />}
            {/* Deployed URL surfaced from AppHealthChecker (Indie step 6, W11-A2) */}
            {app.deployedURL && (
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontSize: 12, color: t.textMute, minWidth: 80 }}>Deployed URL</span>
                <a
                  href={app.deployedURL}
                  target="_blank"
                  rel="noopener noreferrer"
                  style={{
                    fontFamily: t.mono,
                    fontSize: 11.5,
                    color: t.accent,
                    textDecoration: 'none',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                  aria-label={`Visit deployed app at ${app.deployedURL}`}
                >
                  {app.deployedURL}
                </a>
              </div>
            )}
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginTop: 6 }}>
              {app.hasWebhook && <Pill tone="cool">webhook</Pill>}
              {app.autoDeploy && <Pill tone="good">auto-deploy</Pill>}
              {drift?.status === 'in_sync' && <Pill tone="good">in sync</Pill>}
              {drift?.status === 'drift' && <Pill tone="warn">drift</Pill>}
            </div>
          </Card>

          {/* Deploy history + one-click rollback (roadmap M3) */}
          {history.length > 0 && (
            <Card style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              <SectionLabel>Deploy history</SectionLabel>
              {history.map((d, i) => (
                <div
                  key={d.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    paddingBottom: 8,
                    borderBottom: i < history.length - 1 ? `1px solid ${t.line}` : 'none',
                  }}
                >
                  <Pill tone={statusTone(d.status)}>{d.status}</Pill>
                  {d.kind === 'rollback' && <Pill tone="cool">rollback</Pill>}
                  <span
                    style={{
                      fontFamily: t.mono,
                      fontSize: 10.5,
                      color: t.textSoft,
                      flex: 1,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                    title={d.imageRef || d.runId}
                  >
                    {d.imageRef ? d.imageRef.split('/').pop() : `run ${d.runId.slice(0, 8)}`}
                  </span>
                  <span style={{ fontSize: 10, color: t.textMute, whiteSpace: 'nowrap' }}>
                    {new Date(d.createdAt).toLocaleString()}
                  </span>
                  {i > 0 && d.status === 'success' && d.kind === 'deploy' && d.imageRef && (
                    <Btn onClick={() => rollback(d.id, d.imageRef!)}>Roll back</Btn>
                  )}
                </div>
              ))}
            </Card>
          )}

          {/* GitHub webhook card — Indie step 5 (W11-A1, PR #50) */}
          <Card style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <SectionLabel>GitHub webhook</SectionLabel>

            {/* Webhook URL — always shown so operators can copy it before
                setting the secret. The URL is deterministic from the origin
                (backend/internal/server/router.go:191 — no new API field). */}
            <div>
              <Label>Webhook endpoint</Label>
              <Input
                type="text"
                value={webhookUrl}
                readOnly
                aria-label="Webhook endpoint URL"
                style={{ fontFamily: t.mono, fontSize: 11.5 }}
              />
              <Btn
                kind="secondary"
                onClick={copyWebhookUrl}
                style={{ marginTop: 8, justifyContent: 'center', width: '100%' }}
              >
                Copy URL
              </Btn>
            </div>

            <div
              style={{
                borderTop: `1px solid ${hexA(t.line, 0.6)}`,
                paddingTop: 12,
                fontSize: 12.5,
                color: t.textSoft,
                lineHeight: 1.5,
              }}
            >
              {app.hasWebhook
                ? 'A webhook secret is set. Rotating it will invalidate any cached secret on the GitHub side.'
                : 'No webhook secret yet. Set one so push events from GitHub trigger deploys.'}
            </div>
            {!showWebhook ? (
              <Btn
                kind="secondary"
                icon="cog"
                onClick={() => setShowWebhook(true)}
                style={{ justifyContent: 'center' }}
              >
                {app.hasWebhook ? 'Rotate webhook secret' : 'Set webhook secret'}
              </Btn>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column' }}>
                <Label>New secret</Label>
                <Input
                  type="text"
                  value={newSecret}
                  onChange={(e) => setNewSecret(e.target.value)}
                  placeholder="paste or generate"
                  style={{ fontFamily: t.mono, fontSize: 11.5 }}
                />
                <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
                  <Btn kind="ghost" onClick={generateSecret} disabled={rotating}>
                    Generate
                  </Btn>
                  <div style={{ flex: 1 }} />
                  <Btn
                    kind="ghost"
                    onClick={() => {
                      setShowWebhook(false);
                      setNewSecret('');
                    }}
                    disabled={rotating}
                  >
                    Cancel
                  </Btn>
                  <Btn
                    kind="primary"
                    onClick={rotateWebhook}
                    disabled={rotating || newSecret.length < 8}
                  >
                    {rotating ? 'Rotating…' : 'Save secret'}
                  </Btn>
                </div>
              </div>
            )}
          </Card>
        </div>

        <Card pad={0}>
          <div
            style={{
              padding: '14px 18px',
              borderBottom: `1px solid ${t.line}`,
              display: 'flex',
              alignItems: 'center',
              gap: 12,
            }}
          >
            <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
              Build & deploy logs
            </span>
            {lastDeploy && (
              <Pill tone="cool">run {lastDeploy.runId.slice(0, 8)}</Pill>
            )}
            <div style={{ flex: 1 }} />
            {deploying && <Pill tone="ember">streaming</Pill>}
          </div>

          {/* Last deploy summary — Indie step 6 (W11-A2, PR #50).
              Visible once a deploy has been triggered this session.
              url is optional (docker-host targets may not expose an ingress URL). */}
          {lastDeploy && (
            <div
              style={{
                padding: '12px 18px',
                borderBottom: `1px solid ${hexA(t.line, 0.5)}`,
                display: 'flex',
                flexDirection: 'column',
                gap: 8,
              }}
            >
              <SectionLabel>Last deploy</SectionLabel>
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'max-content 1fr',
                  gap: '6px 14px',
                  fontSize: 12.5,
                  alignItems: 'center',
                }}
              >
                <span style={{ color: t.textMute }}>Status</span>
                <span>
                  <Pill tone={statusTone(lastDeploy.status)}>{lastDeploy.status}</Pill>
                </span>
                <span style={{ color: t.textMute }}>Run</span>
                <span style={{ fontFamily: t.mono, fontSize: 12, color: t.text }}>
                  {lastDeploy.runId.slice(0, 8)}
                </span>
                {lastDeploy.url && (
                  <>
                    <span style={{ color: t.textMute }}>URL</span>
                    <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span
                        style={{
                          fontFamily: t.mono,
                          fontSize: 11.5,
                          color: t.text,
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                          maxWidth: 300,
                        }}
                      >
                        {lastDeploy.url}
                      </span>
                      <a
                        href={lastDeploy.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        style={{
                          fontFamily: t.mono,
                          fontSize: 11.5,
                          color: t.accent,
                          textDecoration: 'none',
                          whiteSpace: 'nowrap',
                          border: `1px solid ${hexA(t.accent, 0.4)}`,
                          borderRadius: 5,
                          padding: '2px 8px',
                        }}
                        aria-label={`Visit deployed app at ${lastDeploy.url}`}
                      >
                        Visit ↗
                      </a>
                    </span>
                  </>
                )}
              </div>
            </div>
          )}

          <pre
            ref={logRef}
            style={{
              background: t.bg,
              color: t.text,
              fontFamily: t.mono,
              fontSize: 12,
              padding: 16,
              margin: 0,
              maxHeight: 480,
              minHeight: 280,
              overflow: 'auto',
              whiteSpace: 'pre-wrap',
              borderTop: `1px solid ${hexA(t.line, 0.4)}`,
            }}
          >
            {logs || 'Click Deploy to start. Build logs stream here in real time.'}
          </pre>
        </Card>
      </div>
    </div>
  );
}

// HealthBadge renders the post-deploy readiness verdict written by
// the backend AppHealthChecker. "unknown" is the default until the
// first probe runs (or when the target kind has no probe wired) —
// shown as a muted neutral pill so operators learn the page state
// without being alarmed.
function HealthBadge({ app }: { app: AppModel }) {
  const status = app.healthStatus ?? 'unknown';
  const tone: 'good' | 'bad' | 'warn' | 'neutral' =
    status === 'healthy'
      ? 'good'
      : status === 'failed'
        ? 'bad'
        : status === 'degraded'
          ? 'warn'
          : 'neutral';
  const label =
    status === 'healthy'
      ? 'healthy'
      : status === 'failed'
        ? 'unhealthy'
        : status === 'degraded'
          ? 'degraded'
          : 'health unknown';
  return (
    <span style={{ marginLeft: 10, display: 'inline-flex', verticalAlign: 'middle' }} title={app.healthMessage ?? ''}>
      <Pill tone={tone}>{label}</Pill>
    </span>
  );
}
