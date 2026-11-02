import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { vercelApi } from '../../api/vercel';
import { useAuth } from '../../auth/OIDCProvider';
import type { VercelConnection, VercelProject } from '../../types/vercel';
import { timeAgo } from '../../utils/time';
import { vercelSyncStatus } from '../../utils/vercel';
import Badge from '../ui/Badge';
import ConfirmButton from '../ui/ConfirmButton';
import { Actions, Check, Field, FormError, TextInput } from '../ui/form';
import VercelCoverage from '../cloud/VercelCoverage';
import IntegrationAccess from '../catalog/IntegrationAccess';
import '../cloud/vercel.css';

function tokenError(error: unknown, token: string): string {
  const message = error instanceof Error ? error.message : 'Could not save the Vercel connection.';
  return token ? message.split(token).join('[redacted]') : message;
}

function ConnectionEditor({ connection, onSaved, onClose }: { connection: VercelConnection; onSaved: (value: VercelConnection) => void; onClose: () => void }) {
  const [name, setName] = useState(connection.name);
  const [token, setToken] = useState('');
  const [enabled, setEnabled] = useState(connection.enabled);
  const [selected, setSelected] = useState(connection.projectIds);
  const [projects, setProjects] = useState<VercelProject[] | null>(null);
  const [projectError, setProjectError] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [projectSearch, setProjectSearch] = useState('');
  const [projectPage, setProjectPage] = useState(0);
  useEffect(() => {
    let active = true;
    setLoading(true); setProjectError(''); setProjectPage(0);
    void vercelApi.projects(connection.id).then((value) => { if (active) setProjects(value.projects ?? []); })
      .catch((e: Error) => { if (active) { setProjects(null); setProjectError(e.message); } })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [connection.id, refresh]);

  async function save(e: FormEvent) {
    e.preventDefault(); setBusy(true); setError('');
    try {
      const result = await vercelApi.update(connection.id, { version: connection.version, name: name.trim(), enabled,
        ...(token.trim() ? { token: token.trim() } : {}), ...(projects !== null ? { projectIds: selected } : {}) });
      setToken(''); onSaved(result.connection);
    } catch (e) { setError(tokenError(e, token.trim())); }
    finally { setBusy(false); }
  }
  const unavailable = selected.filter((id) => !projects?.some((project) => project.id === id));
  const matchingProjects = (projects ?? []).filter((project) => `${project.name} ${project.id} ${project.framework ?? ''}`.toLowerCase().includes(projectSearch.toLowerCase()));
  const projectPages = Math.max(1, Math.ceil(matchingProjects.length / 25));
  const shownPage = Math.min(projectPage, projectPages - 1);
  const visibleProjects = matchingProjects.slice(shownPage * 25, (shownPage + 1) * 25);
  function toggle(id: string, checked: boolean) { setSelected((previous) => checked ? [...previous, id] : previous.filter((value) => value !== id)); }
  return <form className="vercel-stack" onSubmit={save} aria-label={`Edit ${connection.name}`}>
    <FormError>{error}</FormError>
    <Field label="Vercel connection name"><TextInput required maxLength={100} value={name} disabled={busy} onChange={(e) => setName(e.target.value)} /></Field>
    <Check label="Keep this connection syncing" checked={enabled} onChange={setEnabled} disabled={busy} />
    <Field label="Replacement Vercel token" hint="Leave blank to keep the saved token. Cooker encrypts it on the server and never displays it again."><TextInput type="password" autoComplete="new-password" value={token} disabled={busy} onChange={(e) => setToken(e.target.value)} /></Field>
    <div className="vercel-stack">
      <strong>Projects to sync</strong>
      <p className="vercel-hint">Only selected projects are included ({selected.length}/20). Selecting none pauses imports for this connection.</p>
      {!loading && projects !== null && <>
        <p className="vercel-meta">Current discovery: {projects.length} accessible projects · {selected.length} selected{unavailable.length ? ` · ${unavailable.length} selected projects no longer accessible` : ''}. New projects require selection.</p>
        <Field label="Find Vercel projects"><TextInput maxLength={256} value={projectSearch} onChange={(e) => { setProjectSearch(e.target.value); setProjectPage(0); }} /></Field>
      </>}
      <FormError>{projectError}</FormError>
      {loading ? <p role="status">Loading accessible projects…</p> : projects !== null && <div className="vercel-projects" role="group" aria-label="Projects to sync">
        {visibleProjects.map((project) => <Check key={project.id} label={<>{project.name}{project.framework && <span className="vercel-meta"> · {project.framework}</span>}</>} checked={selected.includes(project.id)} onChange={(checked) => toggle(project.id, checked)} disabled={busy || (selected.length >= 20 && !selected.includes(project.id))} />)}
        {unavailable.map((id) => <Check key={id} label={`${id} · no longer accessible; deselect to remove`} checked onChange={(checked) => toggle(id, checked)} disabled={busy} />)}
        {!projects.length && !unavailable.length && <p>No accessible projects found for this account or team.</p>}
        {projects.length > 0 && !matchingProjects.length && <p>No accessible projects match this search.</p>}
      </div>}
      {!loading && projects !== null && matchingProjects.length > 0 && <Actions>
        <button type="button" className="hud-btn" disabled={busy || shownPage === 0} onClick={() => setProjectPage(shownPage - 1)}>Previous project page</button>
        <span className="vercel-meta" role="status">Page {shownPage + 1} of {projectPages} · {matchingProjects.length} matching projects</span>
        <button type="button" className="hud-btn" disabled={busy || shownPage + 1 >= projectPages} onClick={() => setProjectPage(shownPage + 1)}>Next project page</button>
      </Actions>}
      <button type="button" className="hud-btn" disabled={busy || loading} onClick={() => setRefresh((value) => value + 1)}>Refresh project list</button>
    </div>
    <Actions>
      <button className="hud-btn hud-btn-primary" disabled={busy || loading || !name.trim() || selected.length > 20}>{busy ? 'Saving…' : 'Save Vercel settings'}</button>
      <button type="button" className="hud-btn" disabled={busy} onClick={() => { setToken(''); onClose(); }}>Cancel changes</button>
    </Actions>
  </form>;
}

export default function VercelConnections() {
  const admin = useAuth().user?.roles.includes('admin');
  const [items, setItems] = useState<VercelConnection[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [loadError, setLoadError] = useState('');
  const [busy, setBusy] = useState('');
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<VercelConnection | null>(null);
  const [name, setName] = useState('');
  const [teamId, setTeamId] = useState('');
  const [token, setToken] = useState('');
  const mutation = useRef(0);
  const load = useCallback(async () => {
    const generation = mutation.current;
    const result = await vercelApi.connections();
    if (generation === mutation.current) { setItems(result.connections ?? []); setLoadError(''); }
  }, []);
  useEffect(() => {
    if (!admin) return;
    let active = true;
    let pending = false;
    const refresh = async () => {
      if (pending) return;
      pending = true;
      const generation = mutation.current;
      try { const result = await vercelApi.connections(); if (active && generation === mutation.current) { setItems(result.connections ?? []); setLoadError(''); } }
      catch (e) { if (active && generation === mutation.current) setLoadError(e instanceof Error ? e.message : 'Could not load Vercel connections.'); }
      finally { pending = false; if (active) setLoading(false); }
    };
    void refresh();
    const timer = window.setInterval(() => void refresh(), 30_000);
    return () => { active = false; window.clearInterval(timer); };
  }, [admin]);
  if (!admin) return <p>Ask an administrator to connect Vercel. You can view connected projects in <Link className="vercel-inline-link" to="/cloud">Cloud</Link>.</p>;
  function replace(connection: VercelConnection) {
    mutation.current++;
    setItems((previous) => previous.some((item) => item.id === connection.id) ? previous.map((item) => item.id === connection.id ? connection : item) : [...previous, connection]);
  }
  async function create(e: FormEvent) {
    e.preventDefault(); setBusy('create'); setError(''); setNotice('');
    try {
      const result = await vercelApi.create({ name: name.trim(), token: token.trim(), ...(teamId.trim() ? { teamId: teamId.trim() } : {}) });
      setToken(''); setName(''); setTeamId(''); setOpen(false); replace(result.connection); setEditing(result.connection);
      setNotice('Connected. Choose the projects you want Cooker to sync.');
    } catch (e) { setError(tokenError(e, token.trim())); }
    finally { setBusy(''); }
  }
  async function sync(connection: VercelConnection) {
    setBusy(connection.id); setError(''); setNotice('');
    try { const result = await vercelApi.sync(connection.id); replace(result.connection); }
    catch (e) { setError(e instanceof Error ? e.message : 'Could not sync Vercel.'); }
    finally { setBusy(''); }
  }
  async function disconnect(connection: VercelConnection) {
    setBusy(connection.id); setError(''); setNotice('');
    try {
      await vercelApi.disconnect(connection.id, connection.version);
      mutation.current++;
      setItems((previous) => previous.filter((item) => item.id !== connection.id));
      if (editing?.id === connection.id) setEditing(null);
      setNotice('Disconnected from Cooker. Your Vercel projects and token are unchanged.');
    } catch (e) { setError(e instanceof Error ? e.message : 'Could not disconnect Vercel.'); }
    finally { setBusy(''); }
  }
  return <div className="vercel-stack" id="vercel-connections">
    <p>Connect your Vercel account or team to see existing deployments in <Link className="vercel-inline-link" to="/cloud">Cloud</Link>. Sync reads project status and recent history. Publishing the prepared Cooker landing is a separate action that requires an explicit review in Cloud.</p>
    <FormError>{error}</FormError>
    <FormError>{loadError}</FormError>
    {(error || loadError) && <button type="button" className="hud-btn" onClick={() => { setError(''); void load().catch((e: Error) => setLoadError(e.message)); }}>Reload Vercel connections</button>}
    {notice && <p role="status" className="form-ok">{notice}</p>}
    {loading && <p>Loading Vercel connections…</p>}
    {!loading && !items.length && !error && !loadError && <p className="vercel-hint">No Vercel account is connected yet.</p>}
    {items.map((connection) => {
      const status = vercelSyncStatus(connection);
      return <article key={connection.id} className="vercel-card vercel-stack" aria-label={connection.name}>
        <div className="vercel-heading"><h3>{connection.name}</h3><Badge variant={status.variant}>{status.label}</Badge></div>
        <p className="vercel-meta">{connection.identity.name} · {connection.identity.scope === 'team' ? 'Team' : 'Personal account'} · {connection.projectIds.length} selected projects</p>
        <p className="vercel-meta">Last successful sync: {connection.lastSuccessAt ? timeAgo(connection.lastSuccessAt) : 'Never'}{connection.retryAt ? ` · Retry ${timeAgo(connection.retryAt)}` : ''}</p>
        <VercelCoverage connection={connection} />
        <IntegrationAccess inventory={connection.integrations} compact scope="connection" />
        {connection.syncError && <FormError>{connection.syncError} Previous results may be out of date.</FormError>}
        {editing?.id === connection.id ? <ConnectionEditor key={`${editing.id}:${editing.version}`} connection={editing} onSaved={(value) => { replace(value); setEditing(null); setNotice('Vercel settings saved. Selected projects will sync automatically.'); }} onClose={() => setEditing(null)} /> : <Actions>
          <button type="button" className="hud-btn" disabled={!!busy || !!editing || open} onClick={() => { setEditing(connection); setNotice(''); }}>Manage projects &amp; connection</button>
          <button type="button" className="hud-btn" disabled={!!busy || !!editing || !connection.enabled || !connection.hasToken || !connection.projectIds.length || connection.syncing || Date.parse(connection.retryAt ?? '') > Date.now()} onClick={() => void sync(connection)}>{busy === connection.id || connection.syncing ? 'Syncing…' : 'Sync now'}</button>
          <ConfirmButton disabled={!!busy || !!editing || open} confirmLabel="Disconnect from Cooker?" onConfirm={() => disconnect(connection)}>Disconnect</ConfirmButton>
        </Actions>}
      </article>;
    })}
    {!open ? <button type="button" className="hud-btn" disabled={!!busy || !!editing} onClick={() => { setOpen(true); setNotice(''); }}>Connect Vercel</button> : <form className="vercel-card vercel-stack" onSubmit={create} aria-label="Connect Vercel">
      <Field label="Vercel connection name"><TextInput required maxLength={100} value={name} disabled={!!busy} onChange={(e) => setName(e.target.value)} placeholder="My Vercel team" /></Field>
      <Field label="Vercel team ID" hint="Optional. Leave blank for your personal account. For a team, use its team_… ID and a token with access to that team."><TextInput value={teamId} disabled={!!busy} onChange={(e) => setTeamId(e.target.value)} placeholder="team_…" /></Field>
      <Field label="Vercel access token" hint="Use a token with access to the account or team above. It is encrypted on the Cooker server, never returned to the browser, and cleared from this form after saving or closing."><TextInput type="password" required autoComplete="new-password" value={token} disabled={!!busy} onChange={(e) => setToken(e.target.value)} /></Field>
      <Actions><button className="hud-btn hud-btn-primary" disabled={!!busy || !name.trim() || !token.trim()}>{busy ? 'Connecting…' : 'Verify & connect Vercel'}</button><button type="button" className="hud-btn" disabled={!!busy} onClick={() => { setOpen(false); setToken(''); setError(''); }}>Cancel connection</button></Actions>
    </form>}
    <p className="vercel-hint">Disconnecting only removes this connection and its imported history from Cooker. It does not delete Vercel deployments or revoke the token in Vercel.</p>
  </div>;
}
