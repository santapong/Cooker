import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { useSearchParams } from 'react-router-dom';
import { environmentsApi } from '../api/environments';
import { settingsApi } from '../api/settings';
import { useAuth } from '../auth/OIDCProvider';
import type { Environment } from '../types/environment';
import type { ClusterConfig } from '../types/infra';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import ConfirmButton from '../components/ui/ConfirmButton';
import { useDraftMutationFence } from '../components/environment/useDraftMutationFence';
import EnvironmentEditor from '../components/environment/EnvironmentEditor';
import { Actions, Field, Select, TextArea, TextInput } from '../components/ui/form';
import { useConfigurationDraftStore } from '../stores/configurationDraftStore';
import { configurationError, ordinaryValues, parseConfiguration } from '../utils/environmentEditing';
import { timeAgo } from '../utils/time';

const initialForm = { name: '', order: '1', type: 'namespace' as 'namespace' | 'cluster', clusterId: '', namespace: '', kubeContext: '', strategy: 'manual' as 'auto' | 'manual', approvers: '1', vars: '' };
export default function EnvironmentsPage() {
  const { user } = useAuth();
  const owner = user?.sub ?? '';
  const canEdit = !!user?.roles.some((role) => role === 'operator' || role === 'admin');
  const admin = !!user?.roles.includes('admin');
  const drafts = useConfigurationDraftStore();
  const currentMutation = useDraftMutationFence();
  const stored = (drafts.owner === owner ? drafts.drafts['environment:new'] : undefined) as { form: typeof initialForm; open: boolean } | undefined;
  const form = stored?.form ?? initialForm;
  const open = stored?.open ?? false;
  const setForm = (next: typeof initialForm) => drafts.setDraft(owner, 'environment:new', { form: next, open: true });
  const [query, setQuery] = useSearchParams();
  const selected = query.get('environment');
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [clusters, setClusters] = useState<ClusterConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => { setBusy(false); }, [currentMutation]);
  const [pageLimit, setPageLimit] = useState(100);
  const [hasMore, setHasMore] = useState(false);
  const request = useRef(0);
  const load = useCallback(async () => {
    const generation = ++request.current;
    setLoading(true);
    try {
      const pages: Environment[] = [];
      let more = false;
      for (let offset = 0; offset < pageLimit; offset += 100) {
        const page = await environmentsApi.list({ limit: 100, offset });
        pages.push(...(page ?? [])); more = page?.length === 100;
        if (!more) break;
      }
      if (generation !== request.current) return;
      setEnvs(pages.sort((a, b) => a.order - b.order)); setHasMore(more); setError(null);
    } catch { if (generation === request.current) setError('Environment inventory is unavailable. Last loaded records may be stale.'); }
    finally { if (generation === request.current) setLoading(false); }
  }, [pageLimit]);
  const invalidateLoad = useCallback(() => { request.current++; }, []);
  useEffect(() => { void load(); return invalidateLoad; }, [load, invalidateLoad]);
  useEffect(() => { let active = true; void settingsApi.listClusters().then((rows) => { if (active) setClusters(rows ?? []); }).catch(() => {}); return () => { active = false; }; }, []);
  const create = async (event: FormEvent) => {
    event.preventDefault(); if (!canEdit || busy) return;
    let plainVars: Record<string, string>;
    try { plainVars = parseConfiguration(form.vars); } catch (failure) { setError(failure instanceof Error ? failure.message : 'Review readable configuration.'); return; }
    const generation = drafts.generation;
    setBusy(true); setError(null);
    try {
      const created = await environmentsApi.create({ name: form.name.trim(), order: Number(form.order) || 0, target: { type: form.type, clusterId: form.clusterId, namespace: form.namespace.trim(), kubeContext: form.kubeContext.trim() }, promotion: form.strategy === 'manual' ? { strategy: 'manual', requiredApprovers: Number(form.approvers) || 1 } : { strategy: 'auto' }, plainVars });
      drafts.clearDraftIf(owner, 'environment:new', stored, generation); if (!currentMutation(generation)) return; setQuery({ environment: created.id }); await load();
    } catch (failure) { if (currentMutation(generation)) setError(configurationError(failure)); } finally { if (currentMutation(generation)) setBusy(false); }
  };
  const remove = async (environment: Environment) => {
    if (!admin || busy) return;
    const generation = drafts.generation;
    setBusy(true); setError(null);
    try { await environmentsApi.delete(environment.id); if (!currentMutation(generation)) return; if (selected === environment.id) setQuery({}); await load(); }
    catch (failure) { if (currentMutation(generation)) setError(configurationError(failure)); } finally { if (currentMutation(generation)) setBusy(false); }
  };
  const rows: ChartRow[] = envs.map((environment) => ({ id: environment.id, name: environment.name, sub: environment.target ? `${environment.target.type} · ${environment.target.namespace || environment.target.clusterId || '—'}` : undefined, status: 'idle', meta: [`lane ${environment.order}`, `${Object.keys(ordinaryValues(environment)).length} readable keys`, `${environment.secretKeys?.length ?? 0} saved protected key names`, `created ${timeAgo(environment.createdAt)}`], trailing: <><button type="button" className="hud-btn" onClick={() => setQuery({ environment: environment.id })}>Inspect configuration</button>{admin && <ConfirmButton className="hud-btn" disabled={busy} onConfirm={() => remove(environment)}>Remove environment</ConfirmButton>}</> }));
  const current = envs.find((environment) => environment.id === selected);
  const newButton = canEdit ? <button type="button" className="hud-btn hud-btn-primary" onClick={() => drafts.setDraft(owner, 'environment:new', { form, open: !open })}>{open ? 'Close new environment' : '＋ New environment'}</button> : undefined;
  return <StarChart title="Environments" count={envs.length} rows={rows} hasThumbs={false} loading={loading} error={error} actions={newButton} empty={{ text: 'Readable configuration and protected values belong to environments. Bind the required keys to each supported service or step.', action: newButton }} footer={hasMore && <button type="button" className="hud-btn" disabled={loading} onClick={() => setPageLimit((value) => value + 100)}>Load more environments</button>}>
    <p className="workspace-note">Readable values are visible to signed-in users. Protected values stay masked; administrators manage them with version-checked writes. {hasMore && 'More environments are available.'}</p>
    {open && canEdit && <form className="panel" onSubmit={(event) => void create(event)}><fieldset disabled={busy}><div className="panel-grid">
      <Field label="Name"><TextInput value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="staging" required maxLength={128} /></Field>
      <Field label="Lane" hint="Promotion order, low to high."><TextInput type="number" value={form.order} onChange={(event) => setForm({ ...form, order: event.target.value })} /></Field>
      <Field label="Target"><Select value={form.type} onChange={(event) => setForm({ ...form, type: event.target.value as typeof form.type })} options={[{ value: 'namespace', label: 'Namespace' }, { value: 'cluster', label: 'Whole cluster' }]} /></Field>
      <Field label="Cluster"><Select value={form.clusterId} onChange={(event) => setForm({ ...form, clusterId: event.target.value })} options={[{ value: '', label: 'default' }, ...clusters.map((cluster) => ({ value: cluster.id, label: cluster.name }))]} /></Field>
      <Field label="Namespace"><TextInput value={form.namespace} onChange={(event) => setForm({ ...form, namespace: event.target.value })} /></Field>
      <Field label="Kube context"><TextInput value={form.kubeContext} onChange={(event) => setForm({ ...form, kubeContext: event.target.value })} /></Field>
      <Field label="Promotion"><Select value={form.strategy} onChange={(event) => setForm({ ...form, strategy: event.target.value as typeof form.strategy })} options={[{ value: 'manual', label: 'Manual approval' }, { value: 'auto', label: 'Automatic' }]} /></Field>
      {form.strategy === 'manual' && <Field label="Approvers required"><TextInput type="number" min={1} value={form.approvers} onChange={(event) => setForm({ ...form, approvers: event.target.value })} /></Field>}
    </div><Field label="Initial readable configuration" hint="One KEY=value per line. Passwords and tokens use the protected editor after creation."><TextArea rows={4} value={form.vars} maxLength={65536} onChange={(event) => setForm({ ...form, vars: event.target.value })} /></Field><Actions><button type="submit" className="hud-btn hud-btn-primary" disabled={!form.name.trim()}>Create environment</button>{stored && <ConfirmButton className="hud-btn" confirmLabel="Discard this new environment draft?" onConfirm={() => drafts.clearDraft(owner, 'environment:new')}>Discard new draft</ConfirmButton>}</Actions></fieldset></form>}
    {current && <EnvironmentEditor key={current.id} environment={current} onSaved={load} />}
    {selected && !current && !loading && <p role="alert">The selected environment is not in the loaded inventory. It may be unavailable or on another page; load more or choose another record.</p>}
  </StarChart>;
}
