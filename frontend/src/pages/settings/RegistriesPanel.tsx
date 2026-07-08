import { useEffect, useState } from 'react';
import { settingsApi } from '../../api/settings';
import type { RegistryConfig } from '../../types/infra';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Btn, Card, EmptyState, Input, Label, Pill } from '../../components/ui/atoms';
import { DataTable } from '../../components/ui/DataTable';
import { useToastStore } from '../../stores/toastStore';
import { SectionHeader } from './SectionHeader';

export default function RegistriesPanel() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [items, setItems] = useState<RegistryConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(false);

  const refresh = () => {
    setLoading(true);
    settingsApi
      .listRegistries()
      .then(setItems)
      .catch(() => setItems([]))
      .finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  return (
    <div style={{ display: 'grid', gridTemplateColumns: adding ? '1fr 360px' : '1fr', gap: 22 }}>
      <Card pad={0}>
        <SectionHeader
          title="Image registries"
          count={items.length}
          action={
            <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
              Add registry
            </Btn>
          }
        />
        {loading ? (
          <div style={{ padding: 40, color: t.textMute, textAlign: 'center' }}>Loading…</div>
        ) : items.length === 0 ? (
          <EmptyState
            title="No registries configured."
            body="Connect Cooker to a registry (Docker Hub, GHCR, ECR, GCR) so build → push → deploy can complete."
            action={
              <Btn kind="primary" icon="plus" onClick={() => setAdding(true)}>
                Add the first registry
              </Btn>
            }
          />
        ) : (
          <DataTable
            rows={items}
            rowKey={(r) => r.id || r.name}
            columns={[
              {
                key: 'name',
                header: 'Name',
                render: (r) => (
                  <span style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}>
                    {r.name}
                  </span>
                ),
              },
              {
                key: 'url',
                header: 'URL',
                render: (r) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                    {r.url}
                  </span>
                ),
              },
              {
                key: 'auth',
                header: 'Auth',
                width: '160px',
                render: (r) => (
                  <Pill tone={r.username ? 'good' : 'neutral'}>
                    {r.username ? `as ${r.username}` : 'anonymous'}
                  </Pill>
                ),
              },
            ]}
          />
        )}
      </Card>

      {adding && (
        <AddRegistryPanel
          onCancel={() => setAdding(false)}
          onAdded={() => {
            setAdding(false);
            pushToast({ kind: 'success', message: 'Registry saved.' });
            refresh();
          }}
        />
      )}
    </div>
  );
}

function AddRegistryPanel({ onCancel, onAdded }: { onCancel: () => void; onAdded: () => void }) {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      await settingsApi.addRegistry({ name, url, username: username || undefined, password: password || undefined });
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
          Add a registry
        </span>
      </div>
      <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
        <Label>Name</Label>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="ghcr-prod" />
        <Label>URL</Label>
        <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="ghcr.io/org" />
        <Label>Username (optional)</Label>
        <Input value={username} onChange={(e) => setUsername(e.target.value)} />
        <Label>Token / password (optional)</Label>
        <Input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="••••••••"
        />
        <div style={{ display: 'flex', gap: 10, marginTop: 22, justifyContent: 'flex-end' }}>
          <Btn kind="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Btn>
          <Btn kind="primary" onClick={submit} disabled={busy || !name || !url}>
            {busy ? 'Saving…' : 'Save registry'}
          </Btn>
        </div>
      </div>
    </Card>
  );
}
