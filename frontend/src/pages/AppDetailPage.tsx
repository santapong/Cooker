import { useState, useEffect, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type { AppModel, AppDeployResponse } from '../types/app';

export default function AppDetailPage() {
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
    appsApi.get(id).then(setApp).catch((e) => setError(e.message)).finally(() => setLoading(false));
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
    ws.onclose = () => {
      setDeploying(false);
    };
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

  if (loading) return <div style={styles.loading}>Loading...</div>;
  if (error || !app) return <div style={styles.error}>{error ?? 'app not found'}</div>;

  return (
    <div>
      <div style={styles.header}>
        <div>
          <button style={styles.backBtn} onClick={() => navigate('/apps')}>← Apps</button>
          <h2 style={styles.title}>{app.name}</h2>
          <p style={styles.subtitle}>
            github.com/{app.githubRepo} @ {app.branch} · target: {app.deployTarget.kind}
            {app.deployTarget.namespace ? ` (${app.deployTarget.namespace})` : ''}
          </p>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="btn-secondary" onClick={remove}>Delete</button>
          <button className="btn-primary" onClick={deploy} disabled={deploying}>
            {deploying ? 'Deploying…' : 'Deploy'}
          </button>
        </div>
      </div>

      {lastDeploy && (
        <div style={styles.runMeta}>
          Run ID: <code>{lastDeploy.runId}</code>
        </div>
      )}

      <pre ref={logRef} style={styles.logs}>
        {logs || 'Click Deploy to start. Build logs stream here in real time.'}
      </pre>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  header: { display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 20, gap: 16 },
  title: { fontSize: 22, fontWeight: 700, color: '#f1f5f9', margin: '4px 0' },
  subtitle: { fontSize: 13, color: '#94a3b8', margin: 0 },
  backBtn: {
    background: 'transparent', border: 'none', color: '#93c5fd',
    fontSize: 13, cursor: 'pointer', padding: 0, marginBottom: 4,
  },
  loading: { color: '#94a3b8', textAlign: 'center', padding: 40 },
  error: { color: '#f87171', textAlign: 'center', padding: 40 },
  runMeta: { color: '#94a3b8', fontSize: 13, marginBottom: 12 },
  logs: {
    backgroundColor: '#0b1220', border: '1px solid #334155', borderRadius: 6,
    padding: 16, color: '#e2e8f0', fontFamily: 'ui-monospace, Menlo, monospace',
    fontSize: 12, maxHeight: 480, overflow: 'auto', margin: 0, whiteSpace: 'pre-wrap',
  },
};
