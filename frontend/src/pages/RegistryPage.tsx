import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { registryApi } from '../api/registry';
import { settingsApi } from '../api/settings';
import { appsApi } from '../api/apps';
import { pipelineApi } from '../api/pipelines';
import { ApiError } from '../api/client';
import { useAuth } from '../auth/OIDCProvider';
import type { RegistryConfig } from '../types/infra';
import { readRegistryGrantTargets, registryGrantState, registryVersionKnown, type RegistryGrantTargets } from '../utils/registryAuthority';
import StarChart, { ChartRows, type ChartRow } from '../components/list/StarChart';
import Caps from '../components/ui/Caps';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Check, Field, FormError, TextInput } from '../components/ui/form';
import { pushToast } from '../stores/toastStore';
import '../components/registry/registry-authority.css';

const emptyTargets: RegistryGrantTargets = { items: [], complete: false };
const editable = (r: RegistryConfig) => registryVersionKnown(r) && registryGrantState(r, { kind: 'app', id: '_metadata_check' }) !== 'unknown' && registryGrantState(r, { kind: 'pipeline', id: '_metadata_check' }) !== 'unknown';
const toggle = (ids: string[], id: string, checked: boolean) => checked ? [...ids, id] : ids.filter((value) => value !== id);

export default function RegistryPage() {
  const admin = useAuth().user?.roles.includes('admin') === true;
  const [repos, setRepos] = useState<string[]>([]);
  const [spec, setSpec] = useState<string | null>(null);
  const [registries, setRegistries] = useState<RegistryConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [registryError, setRegistryError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({ name: '', url: '', username: '', password: '' });
  const [editing, setEditing] = useState<RegistryConfig | null>(null);
  const [appIds, setAppIds] = useState<string[]>([]);
  const [pipelineIds, setPipelineIds] = useState<string[]>([]);
  const [apps, setApps] = useState<RegistryGrantTargets>(emptyTargets);
  const [pipelines, setPipelines] = useState<RegistryGrantTargets>(emptyTargets);
  const [targetsLoading, setTargetsLoading] = useState(false);
  const [targetsError, setTargetsError] = useState<string | null>(null);
  const [grantError, setGrantError] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    const [repositoryResult, registryResult] = await Promise.allSettled([
      registryApi.listRepositories() as Promise<{ repositories: string[]; ociSpec?: string }>,
      settingsApi.listRegistries(),
    ]);
    if (repositoryResult.status === 'fulfilled') {
      setRepos(repositoryResult.value?.repositories ?? []);
      setSpec(repositoryResult.value?.ociSpec ?? null);
      setError(null);
    } else setError('Repository inventory could not be read.');
    if (registryResult.status === 'fulfilled' && Array.isArray(registryResult.value)) {
      setRegistries(registryResult.value);
      setRegistryError(null);
    } else setRegistryError('Registry connections could not be read. Last loaded information may be out of date.');
    setLoading(false);
  }, []);
  useEffect(() => { void load(); }, [load]);

  const loadTargets = async () => {
    setTargetsLoading(true);
    setTargetsError(null);
    const [a, p] = await Promise.allSettled([
      readRegistryGrantTargets((page) => appsApi.list(page)),
      readRegistryGrantTargets((page) => pipelineApi.list(page)),
    ]);
    setApps(a.status === 'fulfilled' ? a.value : emptyTargets);
    setPipelines(p.status === 'fulfilled' ? p.value : emptyTargets);
    if (a.status === 'rejected' || p.status === 'rejected') setTargetsError('Some app or native pipeline choices could not be loaded. Existing grants remain selected.');
    setTargetsLoading(false);
  };
  const showGrants = (r: RegistryConfig) => {
    if (!admin || !editable(r) || registryError || busy) return;
    setEditing(r);
    setAppIds([...(r.allowedAppIds ?? [])]);
    setPipelineIds([...(r.allowedPipelineIds ?? [])]);
    setGrantError(null);
    setConflict(false);
    void loadTargets();
  };
  const reloadGrants = async (id: string) => {
    const latest = await settingsApi.listRegistries();
    if (!Array.isArray(latest)) throw new Error('Registry inventory unavailable.');
    setRegistries(latest);
    const current = latest.find((r) => r.id === id);
    if (!current || !editable(current)) {
      setEditing(null);
      pushToast('warning', 'This connection was removed or its grant metadata is unavailable. Refresh before editing.');
    } else {
      setEditing(current);
      setAppIds([...(current.allowedAppIds ?? [])]);
      setPipelineIds([...(current.allowedPipelineIds ?? [])]);
    }
    setConflict(false);
    setRegistryError(null);
  };
  const saveGrants = async (revoke = false) => {
    if (!admin || !editing || !editable(editing) || busy || conflict || registryError) return;
    setBusy(true);
    setGrantError(null);
    try {
      const updated = await settingsApi.updateRegistryGrants(editing.id, { version: editing.version!, allowedAppIds: revoke ? [] : appIds, allowedPipelineIds: revoke ? [] : pipelineIds });
      if (!updated || updated.id !== editing.id || !editable(updated) || updated.version === editing.version) throw new Error('Registry update could not be verified.');
      setRegistries((current) => current.map((r) => r.id === updated.id ? updated : r));
      setEditing(null);
      pushToast('success', revoke ? 'All app and pipeline access was revoked.' : 'Registry access updated.');
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setConflict(true);
        setGrantError('Registry grants changed while you were editing. The latest grants have been reloaded; review them and save again. Your stale edit was not applied.');
        try { await reloadGrants(editing.id); }
        catch { setGrantError('Registry grants changed. The latest grants could not be read. Reload before saving; your stale edit was not applied.'); }
      } else setGrantError('Registry access could not be updated. No success was confirmed.');
    } finally { setBusy(false); }
  };
  const add = async (e: FormEvent) => {
    e.preventDefault();
    if (!admin || busy) return;
    setBusy(true);
    try {
      await settingsApi.addRegistry({ name: form.name.trim(), url: form.url.trim(), username: form.username || undefined, password: form.password || undefined });
      pushToast('success', 'Registry added. It has no app or pipeline grants until you configure access.');
      setOpen(false);
      setForm({ name: '', url: '', username: '', password: '' });
      await load();
    } catch { pushToast('error', 'Registry could not be added. No success was confirmed.'); }
    finally { setBusy(false); }
  };
  const remove = async (r: RegistryConfig) => {
    if (!admin || busy) return;
    setBusy(true);
    try {
      await settingsApi.deleteRegistry(r.id);
      if (editing?.id === r.id) setEditing(null);
      pushToast('info', 'Registry removed.');
      await load();
    } catch { pushToast('error', 'Registry could not be removed.'); }
    finally { setBusy(false); }
  };

  const rows: ChartRow[] = repos.map((name) => ({ id: name, name, status: 'idle', meta: ['OCI repository'] }));
  const regRows: ChartRow[] = registries.map((r) => ({
    id: r.id, name: r.name, sub: r.url, status: r.hasPassword ? 'ok' : 'idle',
    meta: [r.username ? `user ${r.username}` : 'anonymous', r.hasPassword ? 'credentials on file' : 'no credentials', editable(r) ? `${r.allowedAppIds?.length ?? 0} apps · ${r.allowedPipelineIds?.length ?? 0} native pipelines` : 'access metadata unavailable'],
    trailing: admin ? <div className="registry-controls"><button type="button" className="hud-btn" disabled={busy || !!registryError || !editable(r)} onClick={() => showGrants(r)}>Manage {r.name} access</button><ConfirmButton disabled={busy} confirmLabel={`Confirm removal of ${r.name}?`} onConfirm={() => remove(r)}>Remove {r.name}</ConfirmButton></div> : undefined,
  }));
  const missingApps = appIds.filter((id) => !apps.items.some((app) => app.id === id));
  const missingPipelines = pipelineIds.filter((id) => !pipelines.items.some((p) => p.id === id));

  return <StarChart title="Registry" count={repos.length} rows={rows} hasThumbs={false} loading={loading} error={error} empty={{ text: 'No repositories reported by the inventory endpoint.' }} footer={
    <section className="chart-section" aria-label="Registry connections">
      <div className="section-head"><Caps as="h2">Configured registries {spec ? `· ${spec}` : ''}</Caps><span className="spacer" /><button type="button" className="hud-btn" disabled={busy} onClick={() => void load()}>Refresh registry connections</button>{admin && <button type="button" className="hud-btn" disabled={busy} onClick={() => { setOpen((v) => !v); setForm({ name: '', url: '', username: '', password: '' }); }}>{open ? 'Cancel new registry' : '＋ Add registry'}</button>}</div>
      <p className="registry-help">Saving a connection does not authorize every app. Administrators grant access to saved apps or native pipelines; their build plans must then select the connection. Empty selection uses anonymous access.</p>
      {!admin && <p>Only administrators can add, remove, or grant access to registry connections.</p>}
      <FormError>{registryError}</FormError>
      {open && admin && <form className="panel" onSubmit={add}><div className="panel-grid">
        <Field label="Name"><TextInput value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required autoFocus /></Field>
        <Field label="URL"><TextInput value={form.url} placeholder="ghcr.io" onChange={(e) => setForm({ ...form, url: e.target.value })} required /></Field>
        <Field label="Username"><TextInput value={form.username} autoComplete="off" onChange={(e) => setForm({ ...form, username: e.target.value })} /></Field>
        <Field label="Password / token"><TextInput type="password" value={form.password} autoComplete="new-password" onChange={(e) => setForm({ ...form, password: e.target.value })} /></Field>
      </div><Actions><button type="submit" className="hud-btn hud-btn-primary" disabled={busy}>{busy ? 'Adding…' : 'Add registry'}</button></Actions></form>}
      {regRows.length > 0 ? <ChartRows rows={regRows} hasThumbs={false} /> : !registryError && !loading ? <p>No external registries configured.</p> : null}
      {editing && admin && <form className="panel registry-grants" aria-label={`Registry access for ${editing.name}`} onSubmit={(e) => { e.preventDefault(); void saveGrants(); }}>
        <Caps as="h3">Access for {editing.name}</Caps><p className="registry-help">Editing connection version {editing.version}. Access allows this connection to be selected; it does not deploy, pull, or push an image. Revocation cannot undo work already sent to a provider.</p>
        <FormError>{grantError}</FormError><FormError>{targetsError}</FormError>
        {targetsLoading && <p role="status">Loading saved apps and native pipelines…</p>}
        {(!apps.complete || !pipelines.complete) && !targetsLoading && <p className="registry-help">The loaded target inventory is incomplete. Existing grants outside this list are retained.</p>}
        <fieldset disabled={busy || conflict || !!registryError}><legend>Apps allowed to use this connection</legend><div className="registry-targets">{apps.items.map((app) => <Check key={app.id} label={app.name} checked={appIds.includes(app.id)} onChange={(checked) => setAppIds((ids) => toggle(ids, app.id, checked))} />)}{missingApps.map((id) => <Check key={id} label={`Existing app grant: ${id} (not in loaded inventory)`} checked onChange={(checked) => setAppIds((ids) => toggle(ids, id, checked))} />)}{apps.complete && apps.items.length === 0 && missingApps.length === 0 && <p>No saved apps.</p>}</div></fieldset>
        <fieldset disabled={busy || conflict || !!registryError}><legend>Native pipelines allowed to use this connection</legend><div className="registry-targets">{pipelines.items.map((p) => <Check key={p.id} label={p.name} checked={pipelineIds.includes(p.id)} onChange={(checked) => setPipelineIds((ids) => toggle(ids, p.id, checked))} />)}{missingPipelines.map((id) => <Check key={id} label={`Existing pipeline grant: ${id} (not in loaded inventory)`} checked onChange={(checked) => setPipelineIds((ids) => toggle(ids, id, checked))} />)}{pipelines.complete && pipelines.items.length === 0 && missingPipelines.length === 0 && <p>No native pipelines.</p>}</div></fieldset>
        <Actions><button type="submit" className="hud-btn hud-btn-primary" disabled={busy || conflict || !!registryError}>Save registry access</button><ConfirmButton disabled={busy || conflict || !!registryError} confirmLabel="Confirm revoke all access?" onConfirm={() => saveGrants(true)}>Revoke all access</ConfirmButton><button type="button" className="hud-btn" disabled={busy} onClick={() => { setBusy(true); void reloadGrants(editing.id).then(() => setGrantError(null)).catch(() => setGrantError('Latest registry grants could not be read.')).finally(() => setBusy(false)); }}>Reload registry grants</button><button type="button" className="hud-btn" disabled={busy} onClick={() => setEditing(null)}>Cancel access edit</button><button type="button" className="hud-btn" disabled={busy || targetsLoading} onClick={() => void loadTargets()}>Reload grant targets</button></Actions>
      </form>}
    </section>
  } />;
}
