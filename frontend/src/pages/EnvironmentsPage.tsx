import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { environmentsApi } from '../api/environments';
import { settingsApi } from '../api/settings';
import type { Environment } from '../types/environment';
import type { ClusterConfig } from '../types/infra';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Field, FormError, Select, TextArea, TextInput } from '../components/ui/form';
import { pushToast } from '../stores/toastStore';
import { timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

function parseVars(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split('\n')) {
    const i = line.indexOf('=');
    if (i <= 0) continue;
    out[line.slice(0, i).trim()] = line.slice(i + 1).trim();
  }
  return out;
}

export default function EnvironmentsPage() {
  const [envs, setEnvs] = useState<Environment[]>([]);
  const [clusters, setClusters] = useState<ClusterConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({ name: '', order: '1', type: 'namespace' as 'namespace' | 'cluster', clusterId: '', namespace: '', kubeContext: '', strategy: 'manual' as 'auto' | 'manual', approvers: '1', vars: '' });

  const load = useCallback(async () => {
    try {
      const [list, cl] = await Promise.all([environmentsApi.list({ limit: 100 }), settingsApi.listClusters().catch(() => [] as ClusterConfig[])]);
      setEnvs([...(list ?? [])].sort((a, b) => a.order - b.order));
      setClusters(cl ?? []);
      setError(null);
    } catch (e) {
      setError(message(e));
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await environmentsApi.create({
        name: form.name.trim(),
        order: Number(form.order) || 0,
        target: { type: form.type, clusterId: form.clusterId, namespace: form.namespace.trim(), kubeContext: form.kubeContext.trim() },
        promotion: form.strategy === 'manual' ? { strategy: 'manual', requiredApprovers: Number(form.approvers) || 1 } : { strategy: 'auto' },
        variables: parseVars(form.vars),
      });
      pushToast('success', `Environment "${form.name.trim()}" created.`);
      setOpen(false);
      setForm({ ...form, name: '', namespace: '', vars: '' });
      await load();
    } catch (err) {
      pushToast('error', message(err));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (env: Environment) => {
    try {
      await environmentsApi.delete(env.id);
      pushToast('info', `Environment "${env.name}" deleted.`);
      await load();
    } catch (err) {
      pushToast('error', message(err));
    }
  };

  const rows: ChartRow[] = envs.map((env) => {
    const vars = Object.keys(env.variables ?? {}).length;
    const promo = env.promotion?.strategy === 'manual' ? `manual · ${env.promotion.requiredApprovers ?? 1} approvers` : 'auto-promote';
    return {
      id: env.id,
      name: env.name,
      sub: env.target ? `${env.target.type} · ${env.target.namespace || env.target.clusterId || '—'}` : undefined,
      status: 'idle',
      meta: [`lane ${env.order}`, promo, `${vars} ${vars === 1 ? 'var' : 'vars'}`, `created ${timeAgo(env.createdAt)}`],
      trailing: <ConfirmButton onConfirm={() => remove(env)}>Remove</ConfirmButton>,
    };
  });

  const newButton = (
    <button type="button" className="hud-btn hud-btn-primary" onClick={() => setOpen((v) => !v)}>
      {open ? 'Cancel' : '＋ New environment'}
    </button>
  );

  return (
    <StarChart title="Environments" count={envs.length} rows={rows} hasThumbs={false} loading={loading} error={error} actions={newButton} empty={{ text: 'No environments yet. Dev, Staging and Production lanes live here.', action: newButton }}>
      {open && (
        <form className="panel" onSubmit={create}>
          <div className="panel-grid">
            <Field label="Name">
              <TextInput value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="staging" required autoFocus />
            </Field>
            <Field label="Lane" hint="Promotion order, low to high.">
              <TextInput type="number" value={form.order} onChange={(e) => setForm({ ...form, order: e.target.value })} />
            </Field>
            <Field label="Target">
              <Select value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value as 'namespace' | 'cluster' })} options={[{ value: 'namespace', label: 'Namespace' }, { value: 'cluster', label: 'Whole cluster' }]} />
            </Field>
            <Field label="Cluster">
              <Select value={form.clusterId} onChange={(e) => setForm({ ...form, clusterId: e.target.value })} options={[{ value: '', label: 'default' }, ...clusters.map((c) => ({ value: c.id, label: c.name }))]} />
            </Field>
            <Field label="Namespace">
              <TextInput value={form.namespace} onChange={(e) => setForm({ ...form, namespace: e.target.value })} placeholder="web-staging" />
            </Field>
            <Field label="Kube context">
              <TextInput value={form.kubeContext} onChange={(e) => setForm({ ...form, kubeContext: e.target.value })} />
            </Field>
            <Field label="Promotion">
              <Select value={form.strategy} onChange={(e) => setForm({ ...form, strategy: e.target.value as 'auto' | 'manual' })} options={[{ value: 'manual', label: 'Manual approval' }, { value: 'auto', label: 'Automatic' }]} />
            </Field>
            {form.strategy === 'manual' && (
              <Field label="Approvers required">
                <TextInput type="number" min={1} value={form.approvers} onChange={(e) => setForm({ ...form, approvers: e.target.value })} />
              </Field>
            )}
          </div>
          <Field label="Variables" hint="One KEY=value per line. Secrets are added per environment afterwards.">
            <TextArea value={form.vars} onChange={(e) => setForm({ ...form, vars: e.target.value })} placeholder={'LOG_LEVEL=info\nREGION=eu'} />
          </Field>
          <FormError>{null}</FormError>
          <Actions>
            <button type="submit" className="hud-btn hud-btn-primary" disabled={busy || !form.name.trim()}>
              {busy ? 'Creating…' : 'Create environment'}
            </button>
          </Actions>
        </form>
      )}
    </StarChart>
  );
}
