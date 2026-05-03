import { useState, useEffect, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type { AppModel, AppDeployResponse } from '../types/app';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn, Card, Field, PageHeader, Pill } from '../components/ui/atoms';

export default function AppDetailPage() {
  const t = useTheme();
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

  useEffect(() => {
    if (!id) return;
    appsApi
      .get(id)
      .then(setApp)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
    return () => {
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
