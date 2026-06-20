import { useEffect, useRef, useState } from 'react';
import { settingsApi, type SecretsCheckResult } from '../api/settings';
import { tokensApi } from '../api/tokens';
import type { APIToken } from '../types/token';
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
import { useAuth } from '../auth/OIDCProvider';
import LicensePanel from './settings/LicensePanel';

type Tab = 'registries' | 'clusters' | 'secrets' | 'tokens' | 'license';

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
        {(['registries', 'clusters', 'secrets', 'tokens', 'license'] as const).map((id) => (
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

      {tab === 'registries' ? (
        <RegistriesPanel />
      ) : tab === 'clusters' ? (
        <ClustersPanel />
      ) : tab === 'secrets' ? (
        <SecretsPanel />
      ) : tab === 'tokens' ? (
        <TokensPanel />
      ) : (
        <LicensePanel />
      )}
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

// ── Role helpers ─────────────────────────────────────────────────────────────

const ALL_ROLES = ['admin', 'operator', 'approver', 'viewer'] as const;
type Role = (typeof ALL_ROLES)[number];

function mintableRoles(userRoles: string[]): Role[] {
  if (userRoles.includes('admin')) return [...ALL_ROLES];
  if (userRoles.includes('operator')) return ['operator', 'viewer'];
  if (userRoles.includes('approver')) return ['approver', 'viewer'];
  return ['viewer'];
}

function roleTone(role: string): 'accent' | 'good' | 'warn' | 'cool' | 'neutral' {
  switch (role) {
    case 'admin':
      return 'accent';
    case 'operator':
      return 'good';
    case 'approver':
      return 'warn';
    case 'viewer':
      return 'cool';
    default:
      return 'neutral';
  }
}

function fmtDate(iso?: string): string {
  if (!iso) return 'never';
  try {
    return new Date(iso).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    });
  } catch {
    return iso;
  }
}

// ── Show-once modal ───────────────────────────────────────────────────────────

function TokenCreatedModal({ token, onClose }: { token: string; onClose: () => void }) {
  const t = useTheme();
  const [copied, setCopied] = useState(false);
  // Store the reset timer so it can be cancelled on unmount and avoid a
  // setState-after-unmount warning (FE-M SettingsPage).
  const copyTimerRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (copyTimerRef.current !== null) {
        window.clearTimeout(copyTimerRef.current);
      }
    };
  }, []);

  const copy = () => {
    navigator.clipboard.writeText(token).then(
      () => setCopied(true),
      () => {},
    );
    if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current);
    copyTimerRef.current = window.setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="token-modal-title"
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 1000,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: hexA('#000', 0.6),
        backdropFilter: 'blur(4px)',
        WebkitBackdropFilter: 'blur(4px)',
      }}
    >
      <Card
        pad={0}
        style={{ width: '100%', maxWidth: 520, margin: '0 16px' }}
      >
        <div
          style={{
            padding: '14px 18px',
            borderBottom: `1px solid ${t.line}`,
            background: hexA(t.good, 0.06),
          }}
        >
          <span
            id="token-modal-title"
            style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}
          >
            Token created
          </span>
        </div>
        <div style={{ padding: 18, display: 'flex', flexDirection: 'column', gap: 14 }}>
          <p
            style={{
              fontFamily: t.sans,
              fontSize: 13,
              color: t.bad,
              margin: 0,
              padding: '10px 12px',
              background: hexA(t.bad, 0.07),
              border: `1px solid ${hexA(t.bad, 0.3)}`,
              borderRadius: 8,
              lineHeight: 1.55,
            }}
          >
            Copy this token now. It will <strong>not</strong> be shown again after you close this
            dialog.
          </p>
          <div
            style={{
              fontFamily: t.mono,
              fontSize: 13,
              color: t.text,
              background: hexA(t.surfaceSolid, 0.5),
              border: `1px solid ${t.line}`,
              borderRadius: 8,
              padding: '12px 14px',
              wordBreak: 'break-all',
              userSelect: 'all',
            }}
          >
            {token}
          </div>
          <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
            <Btn kind="primary" onClick={copy}>
              {copied ? 'Copied!' : 'Copy token'}
            </Btn>
            <Btn kind="secondary" onClick={onClose}>
              Close
            </Btn>
          </div>
        </div>
      </Card>
    </div>
  );
}

// ── Add-token side panel ──────────────────────────────────────────────────────

const EXPIRY_PRESETS = [
  { label: '7 days', days: 7 },
  { label: '30 days', days: 30 },
  { label: '90 days', days: 90 },
  { label: 'Never', days: 0 },
] as const;

function AddTokenPanel({
  onCancel,
  onCreated,
  availableRoles,
}: {
  onCancel: () => void;
  onCreated: (plaintext: string) => void;
  availableRoles: Role[];
}) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [name, setName] = useState('');
  const [role, setRole] = useState<string>(availableRoles[0] ?? 'viewer');
  const [expiryPreset, setExpiryPreset] = useState<number>(30);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      let expiresAt: string | undefined;
      if (expiryPreset > 0) {
        const d = new Date();
        d.setDate(d.getDate() + expiryPreset);
        expiresAt = d.toISOString();
      }
      const res = await tokensApi.create({ name, role, expiresAt });
      onCreated(res.token);
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card pad={0}>
      <div
        style={{
          padding: '14px 18px',
          borderBottom: `1px solid ${t.line}`,
          background: hexA(t.accent, 0.04),
        }}
      >
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          Create a token
        </span>
      </div>
      <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
        <Label>Name</Label>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="ci-deploy-bot"
          aria-label="Token name"
        />
        <Label>Role</Label>
        <Select
          value={role}
          onChange={(e) => setRole(e.target.value)}
          aria-label="Token role"
        >
          {availableRoles.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </Select>
        <Label>Expiry</Label>
        <Select
          value={String(expiryPreset)}
          onChange={(e) => setExpiryPreset(Number(e.target.value))}
          aria-label="Token expiry"
        >
          {EXPIRY_PRESETS.map((p) => (
            <option key={p.days} value={String(p.days)}>
              {p.label}
            </option>
          ))}
        </Select>
        <div style={{ display: 'flex', gap: 10, marginTop: 22, justifyContent: 'flex-end' }}>
          <Btn kind="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Btn>
          <Btn kind="primary" onClick={submit} disabled={busy || !name || !role}>
            {busy ? 'Creating…' : 'Create token'}
          </Btn>
        </div>
      </div>
    </Card>
  );
}

// ── Tokens panel ──────────────────────────────────────────────────────────────

function TokensPanel() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const { user } = useAuth();
  const [items, setItems] = useState<APIToken[]>([]);
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(false);
  const [newTokenPlaintext, setNewTokenPlaintext] = useState<string | null>(null);

  const roles = mintableRoles(user?.roles ?? []);

  const refresh = () => {
    setLoading(true);
    tokensApi
      .list()
      .then(setItems)
      .catch(() => setItems([]))
      .finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  const revoke = async (id: string, name: string) => {
    if (!window.confirm(`Revoke token "${name}"? This cannot be undone.`)) return;
    try {
      await tokensApi.remove(id);
      pushToast({ kind: 'success', message: `Token "${name}" revoked.` });
      refresh();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  const handleCreated = (plaintext: string) => {
    setAdding(false);
    setNewTokenPlaintext(plaintext);
  };

  const handleModalClose = () => {
    setNewTokenPlaintext(null);
    refresh();
  };

  return (
    <>
      {newTokenPlaintext && (
        <TokenCreatedModal token={newTokenPlaintext} onClose={handleModalClose} />
      )}
      <div style={{ display: 'grid', gridTemplateColumns: adding ? '1fr 360px' : '1fr', gap: 22 }}>
        <Card pad={0}>
          <SectionHeader
            title="API tokens"
            count={items.length}
            action={
              <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
                Create token
              </Btn>
            }
          />
          {loading ? (
            <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
          ) : items.length === 0 ? (
            <EmptyState
              title="No API tokens."
              body="Create a personal access token or service-account token to authenticate CI/CD pipelines and scripts against the Cooker API."
              action={
                <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
                  Create the first token
                </Btn>
              }
            />
          ) : (
            <DataTable
              rows={items}
              rowKey={(r) => r.id}
              columns={[
                {
                  key: 'name',
                  header: 'Name',
                  render: (r) => (
                    <span
                      style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}
                    >
                      {r.name}
                    </span>
                  ),
                },
                {
                  key: 'prefix',
                  header: 'Prefix',
                  width: '160px',
                  render: (r) => (
                    <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                      {r.displayPrefix}
                    </span>
                  ),
                },
                {
                  key: 'role',
                  header: 'Role',
                  width: '120px',
                  render: (r) => <Pill tone={roleTone(r.role)}>{r.role}</Pill>,
                },
                {
                  key: 'lastUsed',
                  header: 'Last used',
                  width: '130px',
                  render: (r) => (
                    <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textMute }}>
                      {fmtDate(r.lastUsedAt)}
                    </span>
                  ),
                },
                {
                  key: 'expiry',
                  header: 'Expires',
                  width: '130px',
                  render: (r) => (
                    <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textMute }}>
                      {fmtDate(r.expiresAt)}
                    </span>
                  ),
                },
                {
                  key: 'actions',
                  header: '',
                  width: '90px',
                  align: 'right',
                  render: (r) => (
                    <Btn kind="danger" onClick={() => revoke(r.id, r.name)}>
                      Revoke
                    </Btn>
                  ),
                },
              ]}
            />
          )}
        </Card>

        {adding && (
          <AddTokenPanel
            availableRoles={roles}
            onCancel={() => setAdding(false)}
            onCreated={handleCreated}
          />
        )}
      </div>
    </>
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
