import { useEffect, useState } from 'react';
import { hostsApi } from '../api/hosts';
import type { Host } from '../types/infra';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import {
  Btn,
  Card,
  EmptyState,
  Field,
  Input,
  Label,
  PageHeader,
  Pill,
  Select,
  StatusDot,
} from '../components/ui/atoms';
import { DataTable } from '../components/ui/DataTable';
import { Icon } from '../components/ui/Icon';
import { useToastStore } from '../stores/toastStore';

export default function HostsPage() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [selected, setSelected] = useState<Host | null>(null);

  const refresh = () => {
    setLoading(true);
    hostsApi
      .list()
      .then(setHosts)
      .catch(() => setHosts([]))
      .finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  const remove = async (h: Host) => {
    if (!confirm(`Delete host "${h.name}"?`)) return;
    try {
      await hostsApi.delete(h.id);
      pushToast({ kind: 'success', message: `Deleted ${h.name}.` });
      refresh();
      setSelected(null);
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    }
  };

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="managed runtimes"
        title="Hosts"
        subtitle="Docker hosts and Kubernetes clusters Cooker can deploy to. Add a host once, then point apps and pipelines at it."
        actions={
          <Btn kind="primary" icon="plus" onClick={() => { setSelected(null); setCreating(true); }}>
            Add host
          </Btn>
        }
      />

      <div style={{ display: 'grid', gridTemplateColumns: selected ? '1fr 360px' : '1fr', gap: 22 }}>
        <Card pad={0}>
          {loading ? (
            <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
          ) : hosts.length === 0 ? (
            // Empty-state two-CTA — W11 §Indie step 2 (PR #66).
            <EmptyState
              title="No hosts registered yet."
              body="Add a Docker daemon endpoint or a Kubernetes cluster to start deploying."
              action={
                <div style={{ display: 'flex', gap: 10, justifyContent: 'center', flexWrap: 'wrap' }}>
                  <Btn kind="primary" icon="plus" onClick={() => setCreating(true)}>
                    Add your first Host
                  </Btn>
                  <a
                    href="https://github.com/santapong/Cooker/blob/main/docs/user-guide/concepts/hosts-and-targets.md"
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 6,
                      padding: '8px 16px',
                      border: '1px solid currentColor',
                      borderRadius: 7,
                      fontSize: 13.5,
                      color: 'inherit',
                      textDecoration: 'none',
                      opacity: 0.7,
                    }}
                  >
                    Hosts &amp; targets guide ↗
                  </a>
                </div>
              }
            />
          ) : (
            <DataTable
              rows={hosts}
              rowKey={(h) => h.id}
              onRowClick={(h) => { setSelected(h); setCreating(false); }}
              columns={[
                {
                  key: 'name',
                  header: 'Host',
                  render: (h) => (
                    <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                      <StatusDot tone={h.kind === 'kubernetes' ? 'cool' : 'accent'} />
                      <div>
                        <div style={{ fontFamily: t.mono, fontSize: 13, color: t.text, fontWeight: 600 }}>
                          {h.name}
                        </div>
                        <div style={{ fontSize: 11, color: t.textMute, marginTop: 2 }}>
                          {h.id.slice(0, 8)}
                        </div>
                      </div>
                    </div>
                  ),
                },
                {
                  key: 'kind',
                  header: 'Kind',
                  width: '140px',
                  render: (h) => (
                    <Pill tone={h.kind === 'kubernetes' ? 'cool' : 'accent'}>{h.kind}</Pill>
                  ),
                },
                {
                  key: 'reach',
                  header: 'Reachability',
                  width: '160px',
                  render: (h) => (
                    <Pill tone={h.reachability === 'tailnet' ? 'good' : 'neutral'}>
                      {h.reachability}
                    </Pill>
                  ),
                },
                {
                  key: 'endpoint',
                  header: 'Endpoint',
                  render: (h) => (
                    <span
                      style={{
                        fontFamily: t.mono,
                        fontSize: 11.5,
                        color: t.textSoft,
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                      }}
                    >
                      {h.dockerEndpoint || h.kubeconfigRef || h.tailnetIp || '—'}
                    </span>
                  ),
                },
                {
                  key: 'actions',
                  header: '',
                  width: '60px',
                  align: 'right',
                  render: () => <Icon name="arrow" size={13} style={{ color: t.textMute }} />,
                },
              ]}
            />
          )}
        </Card>

        {selected && !creating && (
          <Card pad={0}>
            <div
              style={{
                padding: '14px 18px',
                borderBottom: `1px solid ${t.line}`,
                display: 'flex',
                alignItems: 'center',
                gap: 10,
              }}
            >
              <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
                {selected.name}
              </span>
              <Pill tone={selected.kind === 'kubernetes' ? 'cool' : 'accent'}>{selected.kind}</Pill>
              <div style={{ flex: 1 }} />
              <button
                onClick={() => setSelected(null)}
                style={{
                  background: 'transparent',
                  border: `1px solid ${t.line}`,
                  color: t.textSoft,
                  width: 28,
                  height: 28,
                  borderRadius: 6,
                  display: 'grid',
                  placeItems: 'center',
                  cursor: 'pointer',
                }}
              >
                <Icon name="close" size={12} />
              </button>
            </div>
            <div
              style={{
                padding: 18,
                display: 'flex',
                flexDirection: 'column',
                gap: 14,
              }}
            >
              <Field label="Host ID" mono={selected.id} />
              <Field label="Kind" mono={selected.kind} />
              <Field label="Reachability" mono={selected.reachability} />
              {selected.dockerEndpoint && <Field label="Docker endpoint" mono={selected.dockerEndpoint} />}
              {selected.kubeconfigRef && <Field label="Kubeconfig ref" mono={selected.kubeconfigRef} />}
              {selected.tailnetIp && <Field label="Tailnet IP" mono={selected.tailnetIp} />}
              <Field label="Created" mono={new Date(selected.createdAt).toLocaleString()} />

              <div style={{ flex: 1 }} />
              <Btn kind="danger" onClick={() => remove(selected)}>
                Delete host
              </Btn>
            </div>
          </Card>
        )}

        {creating && (
          <CreateHostPanel
            onCancel={() => setCreating(false)}
            onCreated={() => {
              setCreating(false);
              refresh();
            }}
          />
        )}
      </div>
    </div>
  );
}

function CreateHostPanel({ onCancel, onCreated }: { onCancel: () => void; onCreated: () => void }) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [name, setName] = useState('');
  const [kind, setKind] = useState<'docker' | 'kubernetes'>('docker');
  const [reachability, setReachability] = useState<'direct' | 'tailnet'>('direct');
  const [dockerEndpoint, setDockerEndpoint] = useState('');
  const [kubeconfigRef, setKubeconfigRef] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      await hostsApi.create({
        name,
        kind,
        reachability,
        dockerEndpoint: kind === 'docker' ? dockerEndpoint : undefined,
        kubeconfigRef: kind === 'kubernetes' ? kubeconfigRef : undefined,
      });
      pushToast({ kind: 'success', message: `Host "${name}" added.` });
      onCreated();
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
          Add a host
        </span>
      </div>
      <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
        <Label>Name</Label>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="prod-cluster" />

        <Label>Kind</Label>
        <Select value={kind} onChange={(e) => setKind(e.target.value as 'docker' | 'kubernetes')}>
          <option value="docker">Docker host</option>
          <option value="kubernetes">Kubernetes cluster</option>
        </Select>

        <Label>Reachability</Label>
        <Select
          value={reachability}
          onChange={(e) => setReachability(e.target.value as 'direct' | 'tailnet')}
        >
          <option value="direct">Direct (TCP / kubeconfig)</option>
          <option value="tailnet">Tailnet (Tailscale)</option>
        </Select>

        {kind === 'docker' && (
          <>
            <Label>Docker endpoint</Label>
            <Input
              value={dockerEndpoint}
              onChange={(e) => setDockerEndpoint(e.target.value)}
              placeholder="tcp://10.0.0.3:2375"
            />
          </>
        )}
        {kind === 'kubernetes' && (
          <>
            <Label>Kubeconfig secret ref</Label>
            <Input
              value={kubeconfigRef}
              onChange={(e) => setKubeconfigRef(e.target.value)}
              placeholder="cooker/kubeconfig-prod"
            />
          </>
        )}

        <div style={{ display: 'flex', gap: 10, marginTop: 22, justifyContent: 'flex-end' }}>
          <Btn kind="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Btn>
          <Btn kind="primary" onClick={submit} disabled={busy || !name}>
            {busy ? 'Adding…' : 'Add host'}
          </Btn>
        </div>
      </div>
    </Card>
  );
}
