import { useState, useEffect, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type { AppModel, AppDeployResponse } from '../types/app';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn, Card, Field, Input, Label, PageHeader, Pill, SectionLabel } from '../components/ui/atoms';
import { useToastStore } from '../stores/toastStore';

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
  const [logs, setLogs] = useState<string>('');
  const wsRef = useRef<WebSocket | null>(null);
  const logRef = useRef<HTMLPreElement | null>(null);

  // Webhook rotation panel state.
  const [showWebhook, setShowWebhook] = useState(false);
  const [newSecret, setNewSecret] = useState('');
  const [rotating, setRotating] = useState(false);

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
      wsRef.current?.close();
    };
  }, [id]);

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [logs]);

  const deploy = async () => {
    if (!id) return;
    setDeploying(true);
    setLogs('');
    try {
      const res = await appsApi.deploy(id);
      setLastDeploy(res);
      openStream(res.runId);
    } catch (e) {
      setError((e as Error).message);
      setDeploying(false);
    }
  };

  const openStream = (runId: string) => {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
    const url = `${proto}://${window.location.host}/ws/app-run/${runId}`;
    const ws = new WebSocket(url);
    wsRef.current = ws;
    ws.onmessage = (ev) => {
      setLogs((s) => s + (typeof ev.data === 'string' ? ev.data : ''));
    };
    ws.onclose = () => setDeploying(false);
    ws.onerror = () => {
      setLogs((s) => s + '\n[ws] connection error\n');
      setDeploying(false);
    };
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
            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginTop: 6 }}>
              {app.hasWebhook && <Pill tone="cool">webhook</Pill>}
              {app.autoDeploy && <Pill tone="good">auto-deploy</Pill>}
            </div>
          </Card>

          <Card style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <SectionLabel>GitHub webhook</SectionLabel>
            <div style={{ fontSize: 12.5, color: t.textSoft, lineHeight: 1.5 }}>
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
