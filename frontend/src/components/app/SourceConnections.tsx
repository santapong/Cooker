import { useEffect, useState, type FormEvent } from 'react';
import { automationApi, validSourceConnectionMetadata, type SourceConnection } from '../../api/automation';
import { API_ORIGIN } from '../../api/origin';
import { useAuth } from '../../auth/OIDCProvider';
import { Actions, Check, Field, FormError, Select, TextArea, TextInput } from '../ui/form';
import SourceTokenEditor from './SourceTokenEditor';
import { useDraftMutationFence } from '../environment/useDraftMutationFence';
import { useConfigurationDraftStore } from '../../stores/configurationDraftStore';

export default function SourceConnections() {
  const { user } = useAuth();
  const generation = useConfigurationDraftStore((state) => state.generation);
  return <SourceConnectionsForm key={`${user?.sub ?? 'signed-out'}:${user?.roles.join(',') ?? ''}:${generation}`} />;
}

function SourceConnectionsForm() {
  const { user } = useAuth();
  const [items, setItems] = useState<SourceConnection[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const [provider, setProvider] = useState('github');
  const [origin, setOrigin] = useState('');
  const [repos, setRepos] = useState('');
  const [token, setToken] = useState('');
  const [secret, setSecret] = useState('');
  const [username, setUsername] = useState('');
  const [installation, setInstallation] = useState('');
  const [hookMode, setHookMode] = useState('signing');
  const [privateNetwork, setPrivateNetwork] = useState(false);
  const [caPEM, setCAPEM] = useState('');
  const [editing, setEditing] = useState<SourceConnection | null>(null);
  const [notice, setNotice] = useState('');
  const current = useDraftMutationFence();
  const admin = user?.roles.includes('admin');
  useEffect(() => {
    let cancelled = false;
    if (admin) void automationApi.connections().then((r) => { if (!Array.isArray(r) || !r.every(validSourceConnectionMetadata)) throw new Error('Connection metadata unavailable'); if (!cancelled) setItems(r); }).catch(() => { if (!cancelled) setError('Could not read Git connections.'); });
    return () => { cancelled = true; };
  }, [admin, user?.sub]);
  if (!admin) return <p>Ask an administrator to connect a Git provider.</p>;
  async function save(e: FormEvent) {
    const generation = useConfigurationDraftStore.getState().generation;
    e.preventDefault(); if (busy) return; setBusy(true); setError('');
    try {
      const input = { name, provider, origin: provider === 'github' ? 'https://github.com' : origin,
        apiOrigin: provider === 'github' ? 'https://api.github.com' : origin,
        repositories: repos.split(/[\n,]/).map((r) => r.trim()).filter(Boolean), enabled: true,
        authMode: installation && provider === 'github' ? 'github_app' : token ? 'token' : 'anonymous',
        installationId: provider === 'github' && installation ? Number(installation) : undefined,
        token: provider === 'github' && installation ? undefined : token || undefined, username: token && !(provider === 'github' && installation) ? username || undefined : undefined,
        webhookSecret: secret, webhookAuthMode: provider === 'gitlab' ? hookMode : 'signing',
        allowPrivateNetwork: provider !== 'github' && privateNetwork, caPEM: provider !== 'github' ? caPEM || undefined : undefined };
      await automationApi.connect(input);
      if (!current(generation)) return;
      setToken(''); setSecret(''); setOpen(false);
      const latest = await automationApi.connections();
      if (!Array.isArray(latest) || !latest.every(validSourceConnectionMetadata)) throw new Error('Connection metadata unavailable');
      if (current(generation)) setItems(latest);
    } catch { if (current(generation)) setError('Could not connect source. Check the server, credentials and repository allowlist.'); }
    finally { if (current(generation)) setBusy(false); }
  }
  async function reloadConnections() {
    const generation = useConfigurationDraftStore.getState().generation;
    const latest = await automationApi.connections();
    if (!Array.isArray(latest) || !latest.every(validSourceConnectionMetadata) || !latest.some((item) => item.id === editing?.id)) throw new Error('Connection metadata unavailable');
    if (current(generation)) { setItems(latest); setEditing(null); }
  }
  return <>
    <p>Connect a repository once, then choose deployment rules inside each App. Credentials stay on the Cooker server.</p>
    <FormError>{error}</FormError>
    {notice && <p role="status">{notice}</p>}
    {items.map((c) => <div className="kv" key={c.id}>
      <strong>{c.name}</strong><span>{c.provider} · {c.enabled ? 'Enabled' : 'Disabled'}</span>
      <span>Repositories</span><span>{c.repositories.join(', ')}</span>
      <span>Webhook URL</span><code style={{ overflowWrap: 'anywhere' }}>{`${API_ORIGIN || window.location.origin}/webhooks/connections/${c.id}`}</code>
      {editing?.id === c.id ? <SourceTokenEditor key={`${c.id}:${c.version}:${user?.sub}`} connection={c} onClose={() => setEditing(null)} onReload={reloadConnections} onSaved={(saved, count) => { setItems((values) => values.map((value) => value.id === saved.id ? saved : value)); setEditing(null); setNotice(`Token checked for ${count} allowed repositories and replaced. Re-review affected Apps and deployment rules, then revoke the old token at your Git provider.`); }} /> : c.authMode === 'token' ? <button type="button" className="hud-btn" onClick={() => { setEditing(c); setNotice(''); }}>Replace repository token</button> : <small>{c.authMode === 'github_app' ? 'GitHub App credentials are managed by your Cooker operator.' : 'Public repository connection; no token to replace.'}</small>}
    </div>)}
    {!open ? <button type="button" className="hud-btn" onClick={() => setOpen(true)}>Connect source</button> : <form onSubmit={save}><fieldset disabled={busy} style={{ border: 0, padding: 0, margin: 0 }}>
      <Field label="Connection name"><TextInput required value={name} onChange={(e) => setName(e.target.value)} /></Field>
      <Field label="Git provider"><Select value={provider} onChange={(e) => { setProvider(e.target.value); setToken(''); setSecret(''); setUsername(''); setInstallation(''); setOrigin(''); setCAPEM(''); setPrivateNetwork(false); }} options={[{ value: 'github', label: 'GitHub' }, { value: 'gitlab', label: 'GitLab' }, { value: 'gitea', label: 'Gitea' }]} /></Field>
      {provider !== 'github' && <Field label="Server address" hint="The HTTPS origin of your Git server, without an API path."><TextInput type="url" required placeholder="https://git.example.com" value={origin} onChange={(e) => setOrigin(e.target.value)} /></Field>}
      <Field label="Allowed repositories" hint="One owner/repository per line. GitLab also supports nested groups."><TextArea required rows={3} value={repos} onChange={(e) => setRepos(e.target.value)} /></Field>
      {provider === 'github' && <Field label="GitHub App installation ID" hint="Optional. Use an installation already approved by your Cooker operator."><TextInput type="number" min={1} value={installation} onChange={(e) => setInstallation(e.target.value)} /></Field>}
      {(provider !== 'github' || !installation) && <><Field label="Read-only repository token" hint="Leave blank for public repositories. Use a token scoped to these repositories."><TextInput type="password" autoComplete="new-password" value={token} onChange={(e) => setToken(e.target.value)} /></Field>
        {token && <Field label="Git username" hint={provider === 'gitlab' ? 'Defaults to oauth2.' : provider === 'github' ? 'Defaults to x-access-token.' : 'Your Gitea account username.'}><TextInput required={provider === 'gitea'} value={username} onChange={(e) => setUsername(e.target.value)} /></Field>}</>}
      {provider === 'gitlab' && <Field label="Webhook authentication"><Select value={hookMode} onChange={(e) => setHookMode(e.target.value)} options={[{ value: 'signing', label: 'Signing token (new GitLab)' }, { value: 'legacy_token', label: 'Secret token (older GitLab)' }]} /></Field>}
      <Field label={provider === 'gitlab' && hookMode === 'signing' ? 'Webhook signing token' : 'Webhook secret'} hint={provider === 'gitlab' && hookMode === 'signing' ? 'Use the whsec_ signing token generated by GitLab.' : 'Use the same random secret in Cooker and your repository webhook. At least 32 characters.'}><TextInput type="password" autoComplete="new-password" required minLength={32} value={secret} onChange={(e) => setSecret(e.target.value)} /></Field>
      {provider !== 'github' && <details><summary>Private Git server</summary><Check label="Allow this server to use a private network address" checked={privateNetwork} onChange={setPrivateNetwork} /><Field label="Trusted internal CA certificate" hint="Optional PEM certificate. Certificate verification stays enabled."><TextArea rows={3} value={caPEM} onChange={(e) => setCAPEM(e.target.value)} /></Field></details>}
      <Actions><button className="hud-btn" disabled={busy}>{busy ? 'Saving…' : 'Save connection'}</button><button type="button" className="hud-btn" onClick={() => { setOpen(false); setToken(''); setSecret(''); }} disabled={busy}>Cancel</button></Actions>
    </fieldset></form>}
  </>;
}
