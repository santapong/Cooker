import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { appsApi } from '../api/apps';
import type { AppModel } from '../types/app';

export default function AppsPage() {
  const [apps, setApps] = useState<AppModel[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    refresh();
  }, []);

  const refresh = () => {
    setLoading(true);
    appsApi.list().then(setApps).catch(console.error).finally(() => setLoading(false));
  };

  return (
    <div>
      <div style={styles.header}>
        <h2 style={styles.title}>Apps</h2>
        <button className="btn-primary" onClick={() => setCreating(true)}>New App</button>
      </div>

      {loading ? (
        <div style={styles.loading}>Loading...</div>
      ) : apps.length === 0 && !creating ? (
        <div style={styles.empty}>
          <p>No apps yet.</p>
          <p style={{ color: '#94a3b8', fontSize: 14 }}>
            Point Cooker at a GitHub repo and click Deploy.
          </p>
          <button className="btn-primary" onClick={() => setCreating(true)} style={{ marginTop: 12 }}>
            Connect GitHub Repo
          </button>
        </div>
      ) : (
        <div style={styles.grid}>
          {apps.map((a) => (
            <div
              key={a.id}
              style={styles.card}
              onClick={() => navigate(`/apps/${a.id}`)}
            >
              <h3 style={styles.cardTitle}>{a.name}</h3>
              <p style={styles.cardMeta}>github.com/{a.githubRepo} @ {a.branch}</p>
              <p style={styles.cardMeta}>target: {a.deployTarget.kind}</p>
              <div style={styles.cardFooter}>
                {a.hasWebhook && <span style={styles.badge}>webhook</span>}
                {a.autoDeploy && <span style={styles.badge}>auto-deploy</span>}
              </div>
            </div>
          ))}
        </div>
      )}

      {creating && <CreateAppModal onClose={() => setCreating(false)} onCreated={refresh} />}
    </div>
  );
}

function CreateAppModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const [name, setName] = useState('');
  const [repo, setRepo] = useState('');
  const [branch, setBranch] = useState('main');
  const [target, setTarget] = useState<'kubernetes' | 'docker-host'>('kubernetes');
  const [namespace, setNamespace] = useState('default');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setError(null);
    setBusy(true);
    try {
      await appsApi.create({
        name,
        githubRepo: repo,
        branch,
        deployTarget: { kind: target, namespace: target === 'kubernetes' ? namespace : undefined },
        autoDeploy: false,
      });
      onCreated();
      onClose();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={styles.backdrop} onClick={onClose}>
      <div style={styles.modal} onClick={(e) => e.stopPropagation()}>
        <h3 style={{ marginTop: 0, color: '#f1f5f9' }}>New App</h3>
        <label style={styles.label}>Name</label>
        <input style={styles.input} value={name} onChange={(e) => setName(e.target.value)} placeholder="my-web" />
        <label style={styles.label}>GitHub repo (owner/name)</label>
        <input style={styles.input} value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="octocat/Hello-World" />
        <label style={styles.label}>Branch</label>
        <input style={styles.input} value={branch} onChange={(e) => setBranch(e.target.value)} />
        <label style={styles.label}>Deploy target</label>
        <select style={styles.input} value={target} onChange={(e) => setTarget(e.target.value as any)}>
          <option value="kubernetes">Kubernetes</option>
          <option value="docker-host">Docker host</option>
        </select>
        {target === 'kubernetes' && (
          <>
            <label style={styles.label}>Namespace</label>
            <input style={styles.input} value={namespace} onChange={(e) => setNamespace(e.target.value)} />
          </>
        )}
        {error && <div style={styles.error}>{error}</div>}
        <div style={styles.modalFooter}>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>Cancel</button>
          <button className="btn-primary" onClick={submit} disabled={busy || !name || !repo}>
            {busy ? 'Creating...' : 'Create'}
          </button>
        </div>
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  header: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 20 },
  title: { fontSize: 22, fontWeight: 700, color: '#f1f5f9', margin: 0 },
  loading: { color: '#94a3b8', textAlign: 'center', padding: 40 },
  empty: { textAlign: 'center', padding: 60, color: '#f1f5f9' },
  grid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 16 },
  card: {
    padding: 20, backgroundColor: '#1e293b', borderRadius: 8,
    border: '1px solid #334155', cursor: 'pointer', transition: 'border-color 0.15s',
  },
  cardTitle: { fontSize: 16, fontWeight: 600, color: '#f1f5f9', margin: '0 0 8px' },
  cardMeta: { fontSize: 12, color: '#94a3b8', margin: '0 0 4px' },
  cardFooter: { display: 'flex', gap: 6, marginTop: 12 },
  badge: { fontSize: 11, backgroundColor: '#334155', color: '#93c5fd', padding: '2px 8px', borderRadius: 4 },
  backdrop: {
    position: 'fixed', inset: 0, backgroundColor: 'rgba(0,0,0,0.6)',
    display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 10,
  },
  modal: { backgroundColor: '#0f172a', border: '1px solid #334155', borderRadius: 8, padding: 24, width: 420 },
  label: { display: 'block', fontSize: 12, color: '#94a3b8', margin: '12px 0 4px' },
  input: {
    width: '100%', padding: 8, backgroundColor: '#1e293b', color: '#f1f5f9',
    border: '1px solid #334155', borderRadius: 4, fontSize: 14, boxSizing: 'border-box',
  },
  error: { color: '#f87171', fontSize: 13, marginTop: 12 },
  modalFooter: { display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 20 },
};
