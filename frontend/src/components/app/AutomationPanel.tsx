import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { automationApi, type SourceConnection, type TriggerPreview, type TriggerRule } from '../../api/automation';
import { appsApi } from '../../api/apps';
import { useAuth } from '../../auth/OIDCProvider';
import type { AppModel } from '../../types/app';
import { Actions, Field, FormError, Select, TextArea, TextInput } from '../ui/form';
import TriggerHistoryPanel from './TriggerHistoryPanel';

function parseBindings(text: string): Record<string, string[]> {
  const result: Record<string, string[]> = {};
  for (const row of text.split('\n').map((s) => s.trim()).filter(Boolean)) {
    const split = row.indexOf(':');
    if (split < 1) throw new Error('Use service: KEY_ONE, KEY_TWO on each line.');
    const name = row.slice(0, split).trim();
    if (Object.prototype.hasOwnProperty.call(result, name)) throw new Error(`Service ${name} appears more than once.`);
    result[name] = row.slice(split + 1).split(',').map((s) => s.trim()).filter(Boolean);
  }
  return result;
}
const bindingsText = (app: AppModel) => Object.entries(app.buildPlan?.serviceEnvironment ?? {}).map(([s, keys]) => `${s}: ${keys.join(', ')}`).join('\n');

export default function AutomationPanel({ app, onChanged }: { app: AppModel; onChanged: () => Promise<void> }) {
  const { user } = useAuth();
  const admin = user?.roles.includes('admin');
  const [connections, setConnections] = useState<SourceConnection[]>([]);
  const [rules, setRules] = useState<TriggerRule[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [connection, setConnection] = useState('');
  const [kind, setKind] = useState<'branch' | 'tag'>('branch');
  const [match, setMatch] = useState<'exact' | 'glob'>('exact');
  const [pattern, setPattern] = useState(app.branch || 'main');
  const [excluded, setExcluded] = useState('');
  const [ref, setRef] = useState(app.branch || 'main');
  const [approval, setApproval] = useState<'automatic' | 'review'>('review');
  const [preview, setPreview] = useState<TriggerPreview | null>(null);
  const [editing, setEditing] = useState<TriggerRule | null>(null);
  const [bindings, setBindings] = useState(() => bindingsText(app));
  const load = useCallback(async () => {
    if (!admin) return;
    const [c, r] = await Promise.all([automationApi.connections(), automationApi.rules()]);
    setConnections(c ?? []); setRules((r ?? []).filter((v) => v.appId === app.id));
  }, [admin, app.id]);
  useEffect(() => { void load().catch((e: Error) => setError(e.message)); }, [load]);
  useEffect(() => { setPreview(null); }, [connection, kind, match, pattern, excluded, ref, approval, app.version, bindings]);
  useEffect(() => { setBindings(bindingsText(app)); }, [app.id, app.version]);
  useEffect(() => { if (!admin) return; const timer = window.setInterval(() => void load().catch(() => {}), 10000); return () => window.clearInterval(timer); }, [admin, load]);
  async function run(action: () => Promise<void>) { setBusy(true); setError(''); try { await action(); } catch (e) { setError(e instanceof Error ? e.message : 'Action failed'); } finally { setBusy(false); } }
  const fullRef = `refs/${kind === 'tag' ? 'tags' : 'heads'}/${ref}`;
  const draft = (): TriggerRule => ({ id: editing?.id ?? '', version: editing?.version ?? 0, appId: app.id, name: `${app.name} ${kind}`, enabled: true, source: { connectionId: connection }, refKind: kind, match, include: pattern.split(',').map((s) => s.trim()).filter(Boolean), exclude: excluded.split(',').map((s) => s.trim()).filter(Boolean), approval });
  if (!admin) return <p>Ask an administrator to configure branch or release-tag deployment rules.</p>;
  return <>
    <p>A matching change creates a deployment of that exact commit. Changes to deployment files, credentials, or permissions pause for review.</p>
    <FormError>{error}</FormError>
    <p><Link to="/settings">Manage Git connections</Link></p>
    {rules.map((r) => <div key={r.id} className="kv">
      <strong>{r.name}</strong><span>{r.enabled ? r.approval === 'automatic' ? 'Automatic' : 'Review each deployment' : 'Disabled'} · {r.refKind}: {r.include.join(', ')}</span>
      <Actions><button className="hud-btn" disabled={busy} onClick={() => { setEditing(r); setConnection(r.source.connectionId); setKind(r.refKind); setMatch(r.match); setPattern(r.include.join(', ')); setExcluded((r.exclude ?? []).join(', ')); setApproval(r.approval); setPreview(null); }}>Edit & review</button>
        {r.enabled && <button className="hud-btn" disabled={busy} onClick={() => void run(async () => { await automationApi.disable(r.id); await load(); })}>Disable</button>}</Actions>
    </div>)}
    {app.buildPlan?.kind !== 'compose' ? <p>Automation currently supports reviewed Compose deployments. Select a Compose plan in the App’s source settings.</p> : <>
      <details open={app.buildPlan.environmentScope !== 'service'}><summary>Configuration for each service</summary>
        <p>List the environment keys each service may receive. Values remain in the linked environment. Services omitted here receive no linked keys.</p>
        <Field label="Service permissions" hint="One service per line. Example: n8n-web: DB_HOST, DB_PASSWORD. Leave blank to grant no linked variables."><TextArea rows={4} value={bindings} onChange={(e) => setBindings(e.target.value)} /></Field>
        <button className="hud-btn" disabled={busy} onClick={() => void run(async () => { await appsApi.update(app.id, { ...app, buildPlan: { ...app.buildPlan!, environmentScope: 'service', serviceEnvironment: parseBindings(bindings) } }); setPreview(null); await onChanged(); })}>Save service permissions</button>
      </details>
      <Field label="Source connection"><Select value={connection} onChange={(e) => setConnection(e.target.value)} options={[{ value: '', label: 'Choose a connection' }, ...connections.filter((c) => c.enabled).map((c) => ({ value: c.id, label: `${c.name} · ${c.provider}` }))]} /></Field>
      <Field label="Watch"><Select value={kind} onChange={(e) => setKind(e.target.value as 'branch' | 'tag')} options={[{ value: 'branch', label: 'Branch updates' }, { value: 'tag', label: 'Release tags' }]} /></Field>
      <Field label="Match"><Select value={match} onChange={(e) => setMatch(e.target.value as 'exact' | 'glob')} options={[{ value: 'exact', label: 'Exact names' }, { value: 'glob', label: 'Name patterns' }]} /></Field>
      <Field label="Include" hint={match === 'glob' ? 'Comma-separated patterns. * matches within one path segment; use release/* for release branches.' : 'Comma-separated names, without refs/heads/ or refs/tags/.'}><TextInput value={pattern} onChange={(e) => setPattern(e.target.value)} /></Field>
      <Field label="Exclude" hint="Optional names or patterns. Exclusions always win."><TextInput value={excluded} onChange={(e) => setExcluded(e.target.value)} /></Field>
      <Field label="Deployment approval"><Select value={approval} onChange={(e) => setApproval(e.target.value as 'automatic' | 'review')} options={[{ value: 'review', label: 'Review each deployment' }, { value: 'automatic', label: 'Automatic within approved configuration' }]} /></Field>
      <Field label={kind === 'branch' ? 'Branch to inspect now' : 'Tag to inspect now'} hint="Use a real matching name, even when the rule uses a pattern."><TextInput value={ref} onChange={(e) => setRef(e.target.value)} /></Field>
      <Actions><button className="hud-btn" disabled={busy || !connection || !pattern || !ref || app.buildPlan.environmentScope !== 'service'} onClick={() => void run(async () => setPreview(await automationApi.preview(draft(), fullRef)))}>{busy ? 'Working…' : 'Preview rule'}</button>
        {editing && <button className="hud-btn" disabled={busy} onClick={() => { setEditing(null); setPreview(null); }}>New rule</button>}</Actions>
      {preview && <div className="section">
        <p>Reviewed commit <code>{preview.envelope.commit.slice(0, 12)}</code></p>
        <p>Services: {preview.envelope.graph.services.map((s) => s.name).join(', ')}.</p>
        <p>Changes to these files require another review: {preview.envelope.files.join(', ')}.</p>
        <button className="hud-btn hud-btn-primary" disabled={busy} onClick={() => void run(async () => { await automationApi.save(preview, fullRef); setPreview(null); setEditing(null); await load(); })}>Approve configuration & save rule</button>
      </div>}
      {editing && <button className="hud-btn" disabled={busy || !ref} onClick={() => void run(async () => { await automationApi.check(editing.id, fullRef); await load(); })}>Check for missed changes on this ref</button>}
    </>}
    <TriggerHistoryPanel appId={app.id} />
  </>;
}
