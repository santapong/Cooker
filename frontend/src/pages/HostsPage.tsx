import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { hostsApi } from '../api/hosts';
import type { Host, HostKind } from '../types/infra';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Check, Field, Select, TextArea, TextInput } from '../components/ui/form';
import { pushToast } from '../stores/toastStore';
import { timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

export default function HostsPage() {
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({ name: '', kind: 'docker' as HostKind, reachability: 'direct' as 'direct' | 'tailnet', dockerEndpoint: '', kubeconfigRef: '', tailnetIp: '', sshEndpoint: '', sshUser: '', sshPort: '22', sshPrivateKeyPem: '', sshStrictHostKey: true });

  const load = useCallback(async () => {
    try {
      setHosts((await hostsApi.list({ limit: 100 })) ?? []);
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
      await hostsApi.create({
        name: form.name.trim(),
        kind: form.kind,
        reachability: form.reachability,
        dockerEndpoint: form.kind === 'docker' ? form.dockerEndpoint.trim() || undefined : undefined,
        kubeconfigRef: form.kind === 'kubernetes' ? form.kubeconfigRef.trim() || undefined : undefined,
        tailnetIp: form.reachability === 'tailnet' ? form.tailnetIp.trim() || undefined : undefined,
        sshEndpoint: form.kind === 'ssh-docker' ? form.sshEndpoint.trim() || undefined : undefined,
        sshUser: form.kind === 'ssh-docker' ? form.sshUser.trim() || undefined : undefined,
        sshPort: form.kind === 'ssh-docker' ? Number(form.sshPort) || 22 : undefined,
        sshPrivateKeyPem: form.kind === 'ssh-docker' ? form.sshPrivateKeyPem || undefined : undefined,
        sshStrictHostKey: form.kind === 'ssh-docker' ? form.sshStrictHostKey : undefined,
      });
      pushToast('success', `Host "${form.name.trim()}" added.`);
      setOpen(false);
      setForm({ ...form, name: '', dockerEndpoint: '', kubeconfigRef: '', tailnetIp: '', sshEndpoint: '', sshUser: '', sshPrivateKeyPem: '' });
      await load();
    } catch (err) {
      pushToast('error', message(err));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (h: Host) => {
    try {
      await hostsApi.delete(h.id);
      pushToast('info', `Host "${h.name}" removed.`);
      await load();
    } catch (err) {
      pushToast('error', message(err));
    }
  };

  const rows: ChartRow[] = hosts.map((h) => ({
    id: h.id,
    name: h.name,
    sub: h.dockerEndpoint ?? h.sshEndpoint ?? h.tailnetIp ?? h.kubeconfigRef,
    status: 'idle',
    meta: [h.kind, h.reachability, ...(h.kind === 'ssh-docker' ? [h.hasSSHPrivateKey ? 'key on file' : 'no key'] : []), `updated ${timeAgo(h.updatedAt)}`],
    trailing: <ConfirmButton onConfirm={() => remove(h)}>Remove</ConfirmButton>,
  }));

  const newButton = (
    <button type="button" className="hud-btn hud-btn-primary" onClick={() => setOpen((v) => !v)}>
      {open ? 'Cancel' : '＋ New host'}
    </button>
  );

  return (
    <StarChart title="Hosts" count={hosts.length} rows={rows} hasThumbs={false} loading={loading} error={error} actions={newButton} empty={{ text: 'No hosts yet. Docker daemons, SSH hosts and clusters Cooker can deploy to live here.', action: newButton }}>
      {open && (
        <form className="panel" onSubmit={create}>
          <div className="panel-grid">
            <Field label="Name">
              <TextInput value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="pi-lab" required autoFocus />
            </Field>
            <Field label="Kind">
              <Select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as HostKind })} options={[{ value: 'docker', label: 'Docker daemon' }, { value: 'ssh-docker', label: 'Docker over SSH' }, { value: 'kubernetes', label: 'Kubernetes' }]} />
            </Field>
            <Field label="Reachability">
              <Select value={form.reachability} onChange={(e) => setForm({ ...form, reachability: e.target.value as 'direct' | 'tailnet' })} options={[{ value: 'direct', label: 'Direct' }, { value: 'tailnet', label: 'Tailnet' }]} />
            </Field>
            {form.reachability === 'tailnet' && (
              <Field label="Tailnet IP">
                <TextInput value={form.tailnetIp} onChange={(e) => setForm({ ...form, tailnetIp: e.target.value })} placeholder="100.64.0.12" />
              </Field>
            )}
            {form.kind === 'docker' && (
              <Field label="Docker endpoint">
                <TextInput value={form.dockerEndpoint} onChange={(e) => setForm({ ...form, dockerEndpoint: e.target.value })} placeholder="tcp://10.0.0.5:2376" />
              </Field>
            )}
            {form.kind === 'kubernetes' && (
              <Field label="Kubeconfig ref" hint="Name of a cluster from Settings.">
                <TextInput value={form.kubeconfigRef} onChange={(e) => setForm({ ...form, kubeconfigRef: e.target.value })} />
              </Field>
            )}
            {form.kind === 'ssh-docker' && (
              <>
                <Field label="SSH endpoint">
                  <TextInput value={form.sshEndpoint} onChange={(e) => setForm({ ...form, sshEndpoint: e.target.value })} placeholder="host:22" />
                </Field>
                <Field label="SSH user">
                  <TextInput value={form.sshUser} onChange={(e) => setForm({ ...form, sshUser: e.target.value })} placeholder="deploy" />
                </Field>
                <Field label="SSH port">
                  <TextInput type="number" value={form.sshPort} onChange={(e) => setForm({ ...form, sshPort: e.target.value })} />
                </Field>
              </>
            )}
          </div>
          {form.kind === 'ssh-docker' && (
            <>
              <Field label="Private key (PEM)" hint="Stored encrypted; never echoed back.">
                <TextArea value={form.sshPrivateKeyPem} onChange={(e) => setForm({ ...form, sshPrivateKeyPem: e.target.value })} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />
              </Field>
              <Check label="Verify the host key strictly" checked={form.sshStrictHostKey} onChange={(v) => setForm({ ...form, sshStrictHostKey: v })} />
            </>
          )}
          <Actions>
            <button type="submit" className="hud-btn hud-btn-primary" disabled={busy || !form.name.trim()}>
              {busy ? 'Adding…' : 'Add host'}
            </button>
          </Actions>
        </form>
      )}
    </StarChart>
  );
}
