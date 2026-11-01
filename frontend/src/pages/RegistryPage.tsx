import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { registryApi } from '../api/registry';
import { settingsApi } from '../api/settings';
import type { RegistryConfig } from '../types/infra';
import StarChart, { ChartRows, type ChartRow } from '../components/list/StarChart';
import Caps from '../components/ui/Caps';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Field, TextInput } from '../components/ui/form';
import { pushToast } from '../stores/toastStore';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

export default function RegistryPage() {
  const [repos, setRepos] = useState<string[]>([]);
  const [spec, setSpec] = useState<string | null>(null);
  const [registries, setRegistries] = useState<RegistryConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({ name: '', url: '', username: '', password: '' });

  const load = useCallback(async () => {
    try {
      const [r, regs] = await Promise.all([
        registryApi.listRepositories() as Promise<{ repositories: string[]; ociSpec?: string }>,
        settingsApi.listRegistries().catch(() => [] as RegistryConfig[]),
      ]);
      setRepos(r?.repositories ?? []);
      setSpec(r?.ociSpec ?? null);
      setRegistries(regs ?? []);
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

  const add = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await settingsApi.addRegistry({ name: form.name.trim(), url: form.url.trim(), username: form.username || undefined, password: form.password || undefined });
      pushToast('success', `Registry "${form.name.trim()}" added.`);
      setOpen(false);
      setForm({ name: '', url: '', username: '', password: '' });
      await load();
    } catch (err) {
      pushToast('error', message(err));
    } finally {
      setBusy(false);
    }
  };
  const remove = async (r: RegistryConfig) => {
    try {
      await settingsApi.deleteRegistry(r.id);
      pushToast('info', `Registry "${r.name}" removed.`);
      await load();
    } catch (err) {
      pushToast('error', message(err));
    }
  };

  const rows: ChartRow[] = repos.map((name) => ({ id: name, name, status: 'idle', meta: ['OCI repository'] }));
  const regRows: ChartRow[] = registries.map((r) => ({
    id: r.id,
    name: r.name,
    sub: r.url,
    status: r.hasPassword ? 'ok' : 'idle',
    meta: [r.username ? `user ${r.username}` : 'anonymous', r.hasPassword ? 'credentials on file' : 'no credentials'],
    trailing: <ConfirmButton onConfirm={() => remove(r)}>Remove</ConfirmButton>,
  }));

  return (
    <StarChart
      title="Registry"
      count={repos.length}
      rows={rows}
      hasThumbs={false}
      loading={loading}
      error={error}
      empty={{ text: 'No repositories yet. Images Cooker pushes land here.' }}
      footer={
        <div className="chart-section">
          <div className="section-head">
            <Caps as="h2">Configured registries {spec ? `· ${spec}` : ''}</Caps>
            <span className="spacer" />
            <button type="button" className="hud-btn" onClick={() => setOpen((v) => !v)}>
              {open ? 'Cancel' : '＋ Add registry'}
            </button>
          </div>
          {open && (
            <form className="panel" onSubmit={add}>
              <div className="panel-grid">
                <Field label="Name">
                  <TextInput value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required autoFocus />
                </Field>
                <Field label="URL">
                  <TextInput value={form.url} placeholder="ghcr.io" onChange={(e) => setForm({ ...form, url: e.target.value })} required />
                </Field>
                <Field label="Username">
                  <TextInput value={form.username} autoComplete="off" onChange={(e) => setForm({ ...form, username: e.target.value })} />
                </Field>
                <Field label="Password / token">
                  <TextInput type="password" value={form.password} autoComplete="off" onChange={(e) => setForm({ ...form, password: e.target.value })} />
                </Field>
              </div>
              <Actions>
                <button type="submit" className="hud-btn hud-btn-primary" disabled={busy}>
                  {busy ? 'Adding…' : 'Add registry'}
                </button>
              </Actions>
            </form>
          )}
          {regRows.length ? <ChartRows rows={regRows} hasThumbs={false} /> : <p>No external registries configured.</p>}
        </div>
      }
    />
  );
}
