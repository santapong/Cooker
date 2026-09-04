import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { settingsApi, type SecretsCheckResult } from '../api/settings';
import { environmentsApi } from '../api/environments';
import { tokensApi, type APIToken } from '../api/tokens';
import type { ClusterConfig, RegistryConfig } from '../types/infra';
import type { Environment } from '../types/environment';
import { useLicenseStore } from '../stores/licenseStore';
import { useUIStore } from '../stores/uiStore';
import { pushToast } from '../stores/toastStore';
import Badge from '../components/ui/Badge';
import Caps from '../components/ui/Caps';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Check, Field, FormError, Select, TextArea, TextInput } from '../components/ui/form';
import { timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

function Section({ title, aside, children }: { title: string; aside?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="section">
      <div className="section-head">
        <Caps as="h2">{title}</Caps>
        <span className="spacer" />
        {aside}
      </div>
      {children}
    </section>
  );
}

/** Settings — a single 640px column of small-caps sections (spec §5). */
export default function SettingsPage() {
  const calm = useUIStore((s) => s.calmMode);
  const setCalm = useUIStore((s) => s.setCalmMode);
  const mode = useUIStore((s) => s.mode);
  const setMode = useUIStore((s) => s.setMode);

  // secrets backend
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [envId, setEnvId] = useState('');
  const [testing, setTesting] = useState(false);
  const [secrets, setSecrets] = useState<SecretsCheckResult | null>(null);
  const [secretsError, setSecretsError] = useState<string | null>(null);

  // registries
  const [registries, setRegistries] = useState<RegistryConfig[]>([]);
  const [regForm, setRegForm] = useState({ name: '', url: '', username: '', password: '' });
  const [regOpen, setRegOpen] = useState(false);

  // clusters
  const [clusters, setClusters] = useState<ClusterConfig[]>([]);
  const [clusterForm, setClusterForm] = useState({ name: '', context: '', kubeconfig: '' });
  const [clusterOpen, setClusterOpen] = useState(false);

  // tokens
  const [tokens, setTokens] = useState<APIToken[]>([]);
  const [tokenForm, setTokenForm] = useState({ name: '', role: 'viewer', expiresAt: '' });
  const [tokenOpen, setTokenOpen] = useState(false);
  const [freshToken, setFreshToken] = useState<string | null>(null);

  // license
  const license = useLicenseStore((s) => s.license);
  const licenseStatus = useLicenseStore((s) => s.status);
  const fetchLicense = useLicenseStore((s) => s.fetch);
  const installLicense = useLicenseStore((s) => s.install);
  const removeLicense = useLicenseStore((s) => s.remove);
  const [licenseToken, setLicenseToken] = useState('');

  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    const [e, r, c, t] = await Promise.all([
      environmentsApi.list({ limit: 100 }).catch(() => [] as Environment[]),
      settingsApi.listRegistries().catch(() => [] as RegistryConfig[]),
      settingsApi.listClusters().catch(() => [] as ClusterConfig[]),
      tokensApi.list().catch(() => [] as APIToken[]),
    ]);
    setEnvs(e ?? []);
    setRegistries(r ?? []);
    setClusters(c ?? []);
    setTokens(t ?? []);
  }, []);
  useEffect(() => {
    void load();
    void fetchLicense();
  }, [load, fetchLicense]);

  const run = async (key: string, fn: () => Promise<void>, ok?: string) => {
    setBusy(key);
    setError(null);
    try {
      await fn();
      if (ok) pushToast('success', ok);
    } catch (e) {
      setError(message(e));
      pushToast('error', message(e));
    } finally {
      setBusy(null);
    }
  };

  const testSecrets = async () => {
    setTesting(true);
    setSecretsError(null);
    try {
      setSecrets(await settingsApi.testSecrets(envId || undefined));
    } catch (e) {
      setSecrets(null);
      setSecretsError(message(e));
    } finally {
      setTesting(false);
    }
  };

  const addRegistry = (e: FormEvent) => {
    e.preventDefault();
    void run(
      'registry',
      async () => {
        await settingsApi.addRegistry({ name: regForm.name.trim(), url: regForm.url.trim(), username: regForm.username || undefined, password: regForm.password || undefined });
        setRegForm({ name: '', url: '', username: '', password: '' });
        setRegOpen(false);
        await load();
      },
      'Registry added.',
    );
  };
  const addCluster = (e: FormEvent) => {
    e.preventDefault();
    void run(
      'cluster',
      async () => {
        await settingsApi.addCluster({ name: clusterForm.name.trim(), context: clusterForm.context || undefined, kubeconfig: clusterForm.kubeconfig || undefined });
        setClusterForm({ name: '', context: '', kubeconfig: '' });
        setClusterOpen(false);
        await load();
      },
      'Cluster added.',
    );
  };
  const createToken = (e: FormEvent) => {
    e.preventDefault();
    void run('token', async () => {
      const res = await tokensApi.create({ name: tokenForm.name.trim(), role: tokenForm.role, expiresAt: tokenForm.expiresAt ? new Date(tokenForm.expiresAt).toISOString() : undefined });
      setFreshToken(res.token);
      setTokenForm({ name: '', role: 'viewer', expiresAt: '' });
      setTokenOpen(false);
      await load();
    });
  };

  return (
    <div className="column">
      <h1 tabIndex={-1}>Settings</h1>

      <Section title="Interface">
        <Check label="Calm mode — pause ambient and looping motion" checked={calm} onChange={setCalm} />
        <Field label="Mode" hint="Simple hides advanced controls; Pro shows everything.">
          <Select value={mode} onChange={(e) => setMode(e.target.value as 'simple' | 'pro')} options={[{ value: 'pro', label: 'Pro' }, { value: 'simple', label: 'Simple' }]} />
        </Field>
      </Section>

      <Section title="Secrets backend" aside={secrets && <Badge variant={secrets.ok ? 'ok' : 'fail'}>{secrets.ok ? 'ok' : 'failing'}</Badge>}>
        <p>Round-trips a probe through the configured secrets store.</p>
        <Field label="Environment" hint="Optional — tests the backend that environment resolves to.">
          <Select value={envId} onChange={(e) => setEnvId(e.target.value)} options={[{ value: '', label: 'default' }, ...envs.map((env) => ({ value: env.id, label: env.name }))]} />
        </Field>
        <Actions>
          <button type="button" className="hud-btn" onClick={testSecrets} disabled={testing}>
            {testing ? 'Testing…' : 'Test secrets backend'}
          </button>
          {secrets && (
            <span className="mono" style={{ fontSize: 12, color: 'var(--ink-3)' }}>
              {secrets.backend} · {secrets.latencyMs} ms
            </span>
          )}
        </Actions>
        <FormError>{secretsError}</FormError>
      </Section>

      <Section
        title="Image registries"
        aside={
          <button type="button" className="hud-btn" onClick={() => setRegOpen((v) => !v)}>
            {regOpen ? 'Cancel' : '＋ Add registry'}
          </button>
        }
      >
        {regOpen && (
          <form onSubmit={addRegistry} className="panel">
            <div className="panel-grid">
              <Field label="Name">
                <TextInput value={regForm.name} onChange={(e) => setRegForm({ ...regForm, name: e.target.value })} required autoFocus />
              </Field>
              <Field label="URL">
                <TextInput value={regForm.url} placeholder="ghcr.io" onChange={(e) => setRegForm({ ...regForm, url: e.target.value })} required />
              </Field>
              <Field label="Username">
                <TextInput value={regForm.username} autoComplete="off" onChange={(e) => setRegForm({ ...regForm, username: e.target.value })} />
              </Field>
              <Field label="Password / token">
                <TextInput type="password" value={regForm.password} autoComplete="off" onChange={(e) => setRegForm({ ...regForm, password: e.target.value })} />
              </Field>
            </div>
            <Actions>
              <button type="submit" className="hud-btn hud-btn-primary" disabled={busy === 'registry'}>
                {busy === 'registry' ? 'Adding…' : 'Add'}
              </button>
            </Actions>
          </form>
        )}
        <div className="row-list">
          {registries.length === 0 && <p>No registries configured. Kaniko and BuildKit push anonymously until one is added.</p>}
          {registries.map((r) => (
            <div key={r.id} className="row">
              <span className="grow">
                {r.name} <span className="mono">· {r.url}</span>
              </span>
              <span className="mono">{r.hasPassword ? 'credentials on file' : r.username ? `user ${r.username}` : 'anonymous'}</span>
              <ConfirmButton onConfirm={() => run('del-registry', async () => { await settingsApi.deleteRegistry(r.id); await load(); }, 'Registry removed.')}>
                Remove
              </ConfirmButton>
            </div>
          ))}
        </div>
      </Section>

      <Section
        title="Kubernetes clusters"
        aside={
          <button type="button" className="hud-btn" onClick={() => setClusterOpen((v) => !v)}>
            {clusterOpen ? 'Cancel' : '＋ Add cluster'}
          </button>
        }
      >
        {clusterOpen && (
          <form onSubmit={addCluster} className="panel">
            <div className="panel-grid">
              <Field label="Name">
                <TextInput value={clusterForm.name} onChange={(e) => setClusterForm({ ...clusterForm, name: e.target.value })} required autoFocus />
              </Field>
              <Field label="Context" hint="Optional — a context inside the kubeconfig.">
                <TextInput value={clusterForm.context} onChange={(e) => setClusterForm({ ...clusterForm, context: e.target.value })} />
              </Field>
            </div>
            <Field label="Kubeconfig" hint="Stored encrypted; never echoed back.">
              <TextArea value={clusterForm.kubeconfig} onChange={(e) => setClusterForm({ ...clusterForm, kubeconfig: e.target.value })} placeholder="apiVersion: v1\nkind: Config\n…" />
            </Field>
            <Actions>
              <button type="submit" className="hud-btn hud-btn-primary" disabled={busy === 'cluster'}>
                {busy === 'cluster' ? 'Adding…' : 'Add'}
              </button>
            </Actions>
          </form>
        )}
        <div className="row-list">
          {clusters.length === 0 && <p>No clusters configured. The in-cluster or local kubeconfig is used.</p>}
          {clusters.map((c) => (
            <div key={c.id} className="row">
              <span className="grow">
                {c.name} {c.context && <span className="mono">· {c.context}</span>}
              </span>
              <span className="mono">{c.hasCredentials ? 'kubeconfig on file' : 'no kubeconfig'}</span>
            </div>
          ))}
        </div>
      </Section>

      <Section
        title="API tokens"
        aside={
          <button type="button" className="hud-btn" onClick={() => setTokenOpen((v) => !v)}>
            {tokenOpen ? 'Cancel' : '＋ New token'}
          </button>
        }
      >
        {freshToken && (
          <div className="panel">
            <p>Copy this token now — it is shown once.</p>
            <code className="code">{freshToken}</code>
            <Actions>
              <button type="button" className="hud-btn" onClick={() => setFreshToken(null)}>
                Dismiss
              </button>
            </Actions>
          </div>
        )}
        {tokenOpen && (
          <form onSubmit={createToken} className="panel">
            <div className="panel-grid">
              <Field label="Name">
                <TextInput value={tokenForm.name} onChange={(e) => setTokenForm({ ...tokenForm, name: e.target.value })} required autoFocus />
              </Field>
              <Field label="Role">
                <Select value={tokenForm.role} onChange={(e) => setTokenForm({ ...tokenForm, role: e.target.value })} options={['viewer', 'approver', 'operator', 'admin'].map((r) => ({ value: r, label: r }))} />
              </Field>
              <Field label="Expires" hint="Optional.">
                <TextInput type="date" value={tokenForm.expiresAt} onChange={(e) => setTokenForm({ ...tokenForm, expiresAt: e.target.value })} />
              </Field>
            </div>
            <Actions>
              <button type="submit" className="hud-btn hud-btn-primary" disabled={busy === 'token'}>
                {busy === 'token' ? 'Creating…' : 'Create'}
              </button>
            </Actions>
          </form>
        )}
        <div className="row-list">
          {tokens.length === 0 && <p>No API tokens. Create one for the CLI or a CI job.</p>}
          {tokens.map((t) => (
            <div key={t.id} className="row">
              <span className="grow">
                {t.name} <span className="mono">· {t.displayPrefix}… · {t.role}</span>
              </span>
              <span className="mono">{t.lastUsedAt ? `used ${timeAgo(t.lastUsedAt)}` : 'never used'}{t.expiresAt ? ` · expires ${timeAgo(t.expiresAt)}` : ''}</span>
              <ConfirmButton onConfirm={() => run('del-token', async () => { await tokensApi.remove(t.id); await load(); }, 'Token revoked.')}>
                Revoke
              </ConfirmButton>
            </div>
          ))}
        </div>
      </Section>

      <Section title="License" aside={<Badge variant={licenseStatus === 'active' ? 'ok' : 'muted'}>{license.plan}</Badge>}>
        <div className="kv">
          <Caps>Status</Caps>
          <span className="v">{licenseStatus}</span>
          {license.customer && (
            <>
              <Caps>Customer</Caps>
              <span className="v">{license.customer}</span>
            </>
          )}
          {license.expiresAt && (
            <>
              <Caps>Expires</Caps>
              <span className="v">{timeAgo(license.expiresAt)}</span>
            </>
          )}
          {license.features.length > 0 && (
            <>
              <Caps>Features</Caps>
              <span className="v">{license.features.join(', ')}</span>
            </>
          )}
        </div>
        <Field label="License token">
          <TextArea value={licenseToken} onChange={(e) => setLicenseToken(e.target.value)} placeholder="paste a license token" style={{ minHeight: 60 }} />
        </Field>
        <Actions>
          <button
            type="button"
            className="hud-btn hud-btn-primary"
            disabled={!licenseToken.trim() || busy === 'license'}
            onClick={() => run('license', async () => { await installLicense(licenseToken.trim()); setLicenseToken(''); }, 'License installed.')}
          >
            Install
          </button>
          {licenseStatus !== 'none' && (
            <ConfirmButton className="hud-btn" confirmLabel="Remove license?" onConfirm={() => run('license', async () => { await removeLicense(); }, 'License removed.')}>
              Remove
            </ConfirmButton>
          )}
        </Actions>
      </Section>

      <FormError>{error}</FormError>
    </div>
  );
}
