import { useEffect, useState } from 'react';
import { settingsApi } from '../../api/settings';
import type { ClusterConfig } from '../../types/infra';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Btn, Card, EmptyState, Input, Label } from '../../components/ui/atoms';
import { DataTable } from '../../components/ui/DataTable';
import { useToastStore } from '../../stores/toastStore';
import { SectionHeader } from './SectionHeader';

export default function ClustersPanel() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [items, setItems] = useState<ClusterConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(false);

  const refresh = () => {
    setLoading(true);
    settingsApi
      .listClusters()
      .then(setItems)
      .catch(() => setItems([]))
      .finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  return (
    <div style={{ display: 'grid', gridTemplateColumns: adding ? '1fr 360px' : '1fr', gap: 22 }}>
      <Card pad={0}>
        <SectionHeader
          title="Kubernetes clusters"
          count={items.length}
          action={
            <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
              Add cluster
            </Btn>
          }
        />
        {loading ? (
          <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
        ) : items.length === 0 ? (
          <EmptyState
            title="No clusters configured."
            body="Cooker can talk to multiple Kubernetes clusters via different kubeconfigs."
            action={
              <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
                Add the first cluster
              </Btn>
            }
          />
        ) : (
          <DataTable
            rows={items}
            rowKey={(c) => c.id || c.name}
            columns={[
              {
                key: 'name',
                header: 'Name',
                render: (c) => (
                  <span style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}>
                    {c.name}
                  </span>
                ),
              },
              {
                key: 'context',
                header: 'Kube context',
                render: (c) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                    {c.context || '—'}
                  </span>
                ),
              },
            ]}
          />
        )}
      </Card>

      {adding && (
        <AddClusterPanel
          onCancel={() => setAdding(false)}
          onAdded={() => {
            setAdding(false);
            pushToast({ kind: 'success', message: 'Cluster saved.' });
            refresh();
          }}
        />
      )}
    </div>
  );
}

function AddClusterPanel({ onCancel, onAdded }: { onCancel: () => void; onAdded: () => void }) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [name, setName] = useState('');
  const [contextName, setContextName] = useState('');
  const [kubeconfig, setKubeconfig] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      await settingsApi.addCluster({
        name,
        context: contextName || undefined,
        kubeconfig: kubeconfig || undefined,
      });
      onAdded();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card pad={0}>
      <div style={{ padding: '14px 18px', borderBottom: `1px solid ${t.line}`, background: hexA(t.accent, 0.04) }}>
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          Add a cluster
        </span>
      </div>
      <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
        <Label>Name</Label>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="prod-eu-west" />
        <Label>Context</Label>
        <Input
          value={contextName}
          onChange={(e) => setContextName(e.target.value)}
          placeholder="prod"
        />
        <Label>Kubeconfig (paste, optional)</Label>
        <textarea
          value={kubeconfig}
          onChange={(e) => setKubeconfig(e.target.value)}
          placeholder="apiVersion: v1\nkind: Config\n…"
          rows={6}
          style={{
            width: '100%',
            padding: '9px 11px',
            background: t.bg,
            color: t.text,
            border: `1px solid ${t.line}`,
            borderRadius: 7,
            fontFamily: t.mono,
            fontSize: 12,
            outline: 'none',
            resize: 'vertical',
          }}
        />
        <div style={{ display: 'flex', gap: 10, marginTop: 22, justifyContent: 'flex-end' }}>
          <Btn kind="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Btn>
          <Btn kind="primary" onClick={submit} disabled={busy || !name}>
            {busy ? 'Saving…' : 'Save cluster'}
          </Btn>
        </div>
      </div>
    </Card>
  );
}
