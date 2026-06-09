import { useEffect, useState } from 'react';
import { settingsApi, type SecretsCheckResult } from '../api/settings';
import { environmentsApi } from '../api/environments';
import type { Environment } from '../types/environment';
import type { ClusterConfig, RegistryConfig } from '../types/infra';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import {
  Btn,
  Card,
  EmptyState,
  Input,
  Label,
  PageHeader,
  Pill,
  Select,
} from '../components/ui/atoms';
import { DataTable } from '../components/ui/DataTable';
import { useToastStore } from '../stores/toastStore';

type Tab = 'registries' | 'clusters' | 'secrets';

export default function SettingsPage() {
  const t = useTheme();
  const [tab, setTab] = useState<Tab>('registries');

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="cluster + registry config"
        title="Settings"
        subtitle="Things Cooker needs to talk to: image registries it should authenticate against, Kubernetes contexts it can deploy into."
      />

      <div
        style={{
          display: 'flex',
          padding: 3,
          background: t.surfaceAlt,
          border: `1px solid ${t.line}`,
          borderRadius: 8,
          width: 'fit-content',
          marginBottom: 22,
        }}
      >
        {(['registries', 'clusters', 'secrets'] as const).map((id) => (
          <button
            key={id}
            onClick={() => setTab(id)}
            style={{
              background: tab === id ? t.surface : 'transparent',
              color: tab === id ? t.text : t.textMute,
              fontFamily: t.sans,
              fontSize: 12.5,
              fontWeight: 600,
              letterSpacing: 0.4,
              textTransform: 'uppercase',
              border: 'none',
              padding: '6px 16px',
              borderRadius: 5,
              cursor: 'pointer',
              boxShadow: tab === id ? `0 1px 2px ${hexA('#000', 0.06)}` : 'none',
            }}
          >
            {id}
          </button>
        ))}
      </div>

      {tab === 'registries' ? <RegistriesPanel /> : tab === 'clusters' ? <ClustersPanel /> : <SecretsPanel />}
    </div>
  );
}

function SecretsPanel() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [envId, setEnvId] = useState('');
  const [testing, setTesting] = useState(false);
  const [result, setResult] = useState<SecretsCheckResult | null>(null);

  useEffect(() => {
    environmentsApi
      .list()
      .then(setEnvs)
      .catch(() => setEnvs([]));
  }, []);

  const runTest = async () => {
    setTesting(true);
    setResult(null);
    try {
      setResult(await settingsApi.testSecrets(envId || undefined));
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div style={{ maxWidth: 560 }}>
      <Card pad={0}>
        <SectionHeader title="Secrets backend" count={result ? 1 : 0} />
        <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
          <p style={{ fontFamily: t.sans, fontSize: 13, color: t.textSoft, margin: '0 0 14px' }}>
            Probe the configured secrets backend with a lightweight authenticated read. The check
            reports reachability and latency only — no key names or values are returned.
          </p>
          <Label>Environment to probe (optional)</Label>
          <Select value={envId} onChange={(e) => setEnvId(e.target.value)}>
            <option value="">First environment</option>
            {envs.map((env) => (
              <option key={env.id} value={env.id}>
                {env.name}
              </option>
            ))}
          </Select>
          <div style={{ display: 'flex', gap: 10, marginTop: 18 }}>
            <Btn kind="primary" onClick={runTest} disabled={testing}>
              {testing ? 'Testing…' : 'Test connection'}
            </Btn>
          </div>

          {result && (
            <div
              style={{
                marginTop: 18,
                padding: '12px 14px',
                border: `1px solid ${t.line}`,
                borderRadius: 8,
                background: hexA(result.ok ? '#2e7d32' : '#c62828', 0.05),
                display: 'flex',
                flexDirection: 'column',
                gap: 6,
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                <Pill tone={result.ok ? 'good' : 'bad'}>{result.ok ? 'reachable' : 'failed'}</Pill>
                <span style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}>
                  {result.backend}
                </span>
                <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textMute }}>
                  {result.latencyMs} ms
                </span>
              </div>
              {result.error && (
                <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                  {result.error}
                </span>
              )}
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}

function RegistriesPanel() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [items, setItems] = useState<RegistryConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(false);

  const refresh = () => {
    setLoading(true);
    settingsApi
      .listRegistries()
      .then(setItems)
      .catch(() => setItems([]))
      .finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  return (
    <div style={{ display: 'grid', gridTemplateColumns: adding ? '1fr 360px' : '1fr', gap: 22 }}>
      <Card pad={0}>
        <SectionHeader
          title="Image registries"
          count={items.length}
          action={
            <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
              Add registry
            </Btn>
          }
        />
        {loading ? (
          <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
        ) : items.length === 0 ? (
          <EmptyState
            title="No registries configured."
            body="Connect Cooker to a registry (Docker Hub, GHCR, ECR, GCR) so build → push → deploy can complete."
            action={
              <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
                Add the first registry
              </Btn>
            }
          />
        ) : (
          <DataTable
            rows={items}
            rowKey={(r) => r.id || r.name}
            columns={[
              {
                key: 'name',
                header: 'Name',
                render: (r) => (
                  <span style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}>
                    {r.name}
                  </span>
                ),
              },
              {
                key: 'url',
                header: 'URL',
                render: (r) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                    {r.url}
                  </span>
                ),
              },
              {
                key: 'auth',
                header: 'Auth',
                width: '160px',
                render: (r) => (
                  <Pill tone={r.username ? 'good' : 'neutral'}>
                    {r.username ? `as ${r.username}` : 'anonymous'}
                  </Pill>
                ),
              },
            ]}
          />
        )}
      </Card>

      {adding && (
        <AddRegistryPanel
          onCancel={() => setAdding(false)}
          onAdded={() => {
            setAdding(false);
            pushToast({ kind: 'success', message: 'Registry saved.' });
            refresh();
          }}
        />
      )}
    </div>
  );
}

function AddRegistryPanel({ onCancel, onAdded }: { onCancel: () => void; onAdded: () => void }) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      await settingsApi.addRegistry({ name, url, username: username || undefined, password: password || undefined });
      onAdded();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card pad={0}>
      <div style={{ padding: '14px 18px', borderBottom: `1px solid ${t.line}`, background: hexA(t.accent, 0.04) }}>
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          Add a registry
        </span>
      </div>
      <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
        <Label>Name</Label>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="ghcr-prod" />
        <Label>URL</Label>
        <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="ghcr.io/org" />
        <Label>Username (optional)</Label>
        <Input value={username} onChange={(e) => setUsername(e.target.value)} />
        <Label>Token / password (optional)</Label>
        <Input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="••••••••"
        />
        <div style={{ display: 'flex', gap: 10, marginTop: 22, justifyContent: 'flex-end' }}>
          <Btn kind="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Btn>
          <Btn kind="primary" onClick={submit} disabled={busy || !name || !url}>
            {busy ? 'Saving…' : 'Save registry'}
          </Btn>
        </div>
      </div>
    </Card>
  );
}

function ClustersPanel() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [items, setItems] = useState<ClusterConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(false);

  const refresh = () => {
    setLoading(true);
    settingsApi
      .listClusters()
      .then(setItems)
      .catch(() => setItems([]))
      .finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  return (
    <div style={{ display: 'grid', gridTemplateColumns: adding ? '1fr 360px' : '1fr', gap: 22 }}>
      <Card pad={0}>
        <SectionHeader
          title="Kubernetes clusters"
          count={items.length}
          action={
            <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
              Add cluster
            </Btn>
          }
        />
        {loading ? (
          <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
        ) : items.length === 0 ? (
          <EmptyState
            title="No clusters configured."
            body="Cooker can talk to multiple Kubernetes clusters via different kubeconfigs."
            action={
              <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
                Add the first cluster
              </Btn>
            }
          />
        ) : (
          <DataTable
            rows={items}
            rowKey={(c) => c.id || c.name}
            columns={[
              {
                key: 'name',
                header: 'Name',
                render: (c) => (
                  <span style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}>
                    {c.name}
                  </span>
                ),
              },
              {
                key: 'context',
                header: 'Kube context',
                render: (c) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                    {c.context || '—'}
                  </span>
                ),
              },
            ]}
          />
        )}
      </Card>

      {adding && (
        <AddClusterPanel
          onCancel={() => setAdding(false)}
          onAdded={() => {
            setAdding(false);
            pushToast({ kind: 'success', message: 'Cluster saved.' });
            refresh();
          }}
        />
      )}
    </div>
  );
}

function AddClusterPanel({ onCancel, onAdded }: { onCancel: () => void; onAdded: () => void }) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [name, setName] = useState('');
  const [contextName, setContextName] = useState('');
  const [kubeconfig, setKubeconfig] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      await settingsApi.addCluster({
        name,
        context: contextName || undefined,
        kubeconfig: kubeconfig || undefined,
      });
      onAdded();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card pad={0}>
      <div style={{ padding: '14px 18px', borderBottom: `1px solid ${t.line}`, background: hexA(t.accent, 0.04) }}>
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          Add a cluster
        </span>
      </div>
      <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
        <Label>Name</Label>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="prod-eu-west" />
        <Label>Context</Label>
        <Input
          value={contextName}
          onChange={(e) => setContextName(e.target.value)}
          placeholder="prod"
        />
        <Label>Kubeconfig (paste, optional)</Label>
        <textarea
          value={kubeconfig}
          onChange={(e) => setKubeconfig(e.target.value)}
          placeholder="apiVersion: v1\nkind: Config\n…"
          rows={6}
          style={{
            width: '100%',
            padding: '9px 11px',
            background: t.bg,
            color: t.text,
            border: `1px solid ${t.line}`,
            borderRadius: 7,
            fontFamily: t.mono,
            fontSize: 12,
            outline: 'none',
            resize: 'vertical',
          }}
        />
        <div style={{ display: 'flex', gap: 10, marginTop: 22, justifyContent: 'flex-end' }}>
          <Btn kind="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Btn>
          <Btn kind="primary" onClick={submit} disabled={busy || !name}>
            {busy ? 'Saving…' : 'Save cluster'}
          </Btn>
        </div>
      </div>
    </Card>
  );
}

function SectionHeader({
  title,
  count,
  action,
}: {
  title: string;
  count: number;
  action?: React.ReactNode;
}) {
  const t = useTheme();
  return (
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
        {title}
      </span>
      <Pill>{count}</Pill>
      <div style={{ flex: 1 }} />
      {action}
    </div>
  );
}
