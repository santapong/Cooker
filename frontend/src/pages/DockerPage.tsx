import { useEffect, useState } from 'react';
import { useDockerStore } from '../stores/dockerStore';
import { dockerApi } from '../api/docker';
import type { DockerNetwork, DockerVolume } from '../types/infra';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn, Card, EmptyState, Input, Label, PageHeader, Pill, Select, StatusDot } from '../components/ui/atoms';
import { DataTable } from '../components/ui/DataTable';
import { useToastStore } from '../stores/toastStore';

type Tab = 'images' | 'containers' | 'networks' | 'volumes';

export default function DockerPage() {
  const t = useTheme();
  const { images, containers, loading, fetchImages, fetchContainers } = useDockerStore();
  const [tab, setTab] = useState<Tab>('images');

  useEffect(() => {
    fetchImages();
    fetchContainers();
  }, [fetchImages, fetchContainers]);

  return (
    <div style={{ padding: '26px 28px 60px' }}>
      <PageHeader
        eyebrow="local docker host"
        title="Docker"
        subtitle="Browse images cached on the build host, inspect running containers, manage networks and volumes."
        actions={
          tab === 'images' ? (
            <>
              <Btn kind="secondary" icon="layers">Pull image</Btn>
              <Btn kind="primary" icon="plus">Build image</Btn>
            </>
          ) : tab === 'containers' ? (
            <Btn kind="primary" icon="plus">Run container</Btn>
          ) : null
        }
      />

      <div
        style={{
          display: 'flex',
          padding: 3,
          background: t.surfaceAlt,
          border: `1px solid ${t.line}`,
          borderRadius: 8,
          width: 'fit-content',
          marginBottom: 22,
          gap: 0,
        }}
      >
        {(['images', 'containers', 'networks', 'volumes'] as const).map((id) => (
          <button
            key={id}
            onClick={() => setTab(id)}
            style={{
              background: tab === id ? t.surface : 'transparent',
              color: tab === id ? t.text : t.textMute,
              fontFamily: t.sans,
              fontSize: 12.5,
              fontWeight: 600,
              letterSpacing: 0.4,
              textTransform: 'uppercase',
              border: 'none',
              padding: '6px 16px',
              borderRadius: 5,
              cursor: 'pointer',
              boxShadow: tab === id ? `0 1px 2px ${hexA('#000', 0.06)}` : 'none',
            }}
          >
            {id}
          </button>
        ))}
      </div>

      {tab === 'images' && (
        <Card pad={0}>
          <SectionHeader title="Images" count={images.length} />
          {/* Empty-state two-CTA — W11 §Indie step 2 (PR #66). */}
          {!loading && images.length === 0 ? (
            <EmptyState
              title="No images cached locally."
              body="Make sure the Docker daemon is running and Cooker's host transport is configured."
              action={
                <div style={{ display: 'flex', gap: 10, justifyContent: 'center', flexWrap: 'wrap' }}>
                  <Btn kind="primary" icon="play">
                    Start the Docker daemon
                  </Btn>
                  <a
                    href="https://github.com/santapong/Cooker/blob/main/docs/user-guide/operations/docker-builds.md"
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
                    Docker builds guide ↗
                  </a>
                </div>
              }
            />
          ) : (
          <DataTable
            rows={images}
            rowKey={(img) => img.id}
            empty={loading ? 'Loading…' : 'No images cached locally yet.'}
            columns={[
              {
                key: 'repo',
                header: 'Repository',
                render: (img) => (
                  <span style={{ fontFamily: t.mono, fontSize: 12 }}>
                    {img.repoTags?.[0]?.split(':')[0] || img.id.slice(0, 12)}
                  </span>
                ),
              },
              {
                key: 'tag',
                header: 'Tag',
                width: '160px',
                render: (img) => (
                  <Pill tone="neutral">{img.repoTags?.[0]?.split(':')[1] || 'latest'}</Pill>
                ),
              },
              {
                key: 'size',
                header: 'Size',
                width: '120px',
                align: 'right',
                render: (img) => (
                  <span style={{ fontFamily: t.mono, fontSize: 12 }}>
                    {(img.size / 1024 / 1024).toFixed(1)} MB
                  </span>
                ),
              },
              {
                key: 'created',
                header: 'Created',
                width: '160px',
                render: (img) => (
                  <span style={{ color: t.textMute, fontSize: 12 }}>
                    {new Date(img.created).toLocaleDateString()}
                  </span>
                ),
              },
            ]}
          />
          )}
        </Card>
      )}

      {tab === 'containers' && (
        <Card pad={0}>
          <SectionHeader title="Containers" count={containers.length} />
          <DataTable
            rows={containers}
            rowKey={(c) => c.id}
            empty={loading ? 'Loading…' : 'No containers running.'}
            columns={[
              {
                key: 'name',
                header: 'Name',
                render: (c) => (
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <StatusDot tone={c.state === 'running' ? 'good' : 'neutral'} />
                    <span style={{ fontFamily: t.mono, fontSize: 12 }}>{c.name}</span>
                  </div>
                ),
              },
              {
                key: 'image',
                header: 'Image',
                render: (c) => (
                  <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                    {c.image}
                  </span>
                ),
              },
              {
                key: 'state',
                header: 'State',
                width: '120px',
                render: (c) => (
                  <Pill tone={c.state === 'running' ? 'good' : 'neutral'}>{c.state}</Pill>
                ),
              },
              {
                key: 'status',
                header: 'Status',
                width: '200px',
                render: (c) => (
                  <span style={{ color: t.textSoft, fontSize: 12 }}>{c.status}</span>
                ),
              },
            ]}
          />
        </Card>
      )}

      {tab === 'networks' && <NetworksTab />}
      {tab === 'volumes' && <VolumesTab />}
    </div>
  );
}

function NetworksTab() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [networks, setNetworks] = useState<DockerNetwork[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);

  const refresh = () => {
    setLoading(true);
    dockerApi
      .listNetworks()
      .then(setNetworks)
      .catch(() => setNetworks([]))
      .finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  return (
    <div style={{ display: 'grid', gridTemplateColumns: creating ? '1fr 360px' : '1fr', gap: 22 }}>
      <Card pad={0}>
        <SectionHeader
          title="Networks"
          count={networks.length}
          action={
            <Btn kind="primary" icon="plus" onClick={() => setCreating(true)}>
              Create network
            </Btn>
          }
        />
        <DataTable
          rows={networks}
          rowKey={(n) => n.id || n.name}
          empty={
            loading
              ? 'Loading…'
              : 'No networks reported. Cooker needs a configured Docker host transport (P9.4) before it can list these.'
          }
          columns={[
            {
              key: 'name',
              header: 'Name',
              render: (n) => (
                <span style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}>
                  {n.name}
                </span>
              ),
            },
            {
              key: 'driver',
              header: 'Driver',
              width: '160px',
              render: (n) => <Pill tone="cool">{n.driver}</Pill>,
            },
            {
              key: 'scope',
              header: 'Scope',
              width: '140px',
              render: (n) => <Pill>{n.scope}</Pill>,
            },
            {
              key: 'host',
              header: 'Host',
              render: (n) => (
                <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                  {n.hostId || 'local'}
                </span>
              ),
            },
          ]}
        />
      </Card>

      {creating && (
        <Card pad={0}>
          <div
            style={{
              padding: '14px 18px',
              borderBottom: `1px solid ${t.line}`,
              background: hexA(t.accent, 0.04),
            }}
          >
            <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
              New network
            </span>
          </div>
          <CreateNetworkForm
            onCancel={() => setCreating(false)}
            onCreated={() => {
              setCreating(false);
              pushToast({ kind: 'success', message: 'Network create requested.' });
              refresh();
            }}
          />
        </Card>
      )}
    </div>
  );
}

function CreateNetworkForm({
  onCancel,
  onCreated,
}: {
  onCancel: () => void;
  onCreated: () => void;
}) {
  const pushToast = useToastStore((s) => s.push);
  const [name, setName] = useState('');
  const [driver, setDriver] = useState('bridge');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      await dockerApi.createNetwork({ name, driver });
      onCreated();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
      <Label>Name</Label>
      <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="cooker-net" />
      <Label>Driver</Label>
      <Select value={driver} onChange={(e) => setDriver(e.target.value)}>
        <option value="bridge">bridge</option>
        <option value="overlay">overlay</option>
        <option value="host">host</option>
        <option value="macvlan">macvlan</option>
        <option value="ipvlan">ipvlan</option>
      </Select>
      <div style={{ display: 'flex', gap: 10, marginTop: 22, justifyContent: 'flex-end' }}>
        <Btn kind="ghost" onClick={onCancel} disabled={busy}>
          Cancel
        </Btn>
        <Btn kind="primary" onClick={submit} disabled={busy || !name}>
          {busy ? 'Creating…' : 'Create network'}
        </Btn>
      </div>
    </div>
  );
}

function VolumesTab() {
  const t = useTheme();
  const pushToast = useToastStore((s) => s.push);
  const [volumes, setVolumes] = useState<DockerVolume[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);

  const refresh = () => {
    setLoading(true);
    dockerApi
      .listVolumes()
      .then(setVolumes)
      .catch(() => setVolumes([]))
      .finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  return (
    <div style={{ display: 'grid', gridTemplateColumns: creating ? '1fr 360px' : '1fr', gap: 22 }}>
      <Card pad={0}>
        <SectionHeader
          title="Volumes"
          count={volumes.length}
          action={
            <Btn kind="primary" icon="plus" onClick={() => setCreating(true)}>
              Create volume
            </Btn>
          }
        />
        <DataTable
          rows={volumes}
          rowKey={(v) => v.name}
          empty={
            loading
              ? 'Loading…'
              : 'No volumes reported. Cooker needs a configured Docker host transport (P9.4) before it can list these.'
          }
          columns={[
            {
              key: 'name',
              header: 'Name',
              render: (v) => (
                <span style={{ fontFamily: t.mono, fontSize: 12.5, color: t.text, fontWeight: 600 }}>
                  {v.name}
                </span>
              ),
            },
            {
              key: 'driver',
              header: 'Driver',
              width: '160px',
              render: (v) => <Pill tone="cool">{v.driver}</Pill>,
            },
            {
              key: 'mount',
              header: 'Mountpoint',
              render: (v) => (
                <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                  {v.mountpoint}
                </span>
              ),
            },
            {
              key: 'host',
              header: 'Host',
              width: '120px',
              render: (v) => (
                <span style={{ fontFamily: t.mono, fontSize: 11.5, color: t.textSoft }}>
                  {v.hostId || 'local'}
                </span>
              ),
            },
          ]}
        />
      </Card>

      {creating && (
        <Card pad={0}>
          <div
            style={{
              padding: '14px 18px',
              borderBottom: `1px solid ${t.line}`,
              background: hexA(t.accent, 0.04),
            }}
          >
            <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
              New volume
            </span>
          </div>
          <CreateVolumeForm
            onCancel={() => setCreating(false)}
            onCreated={() => {
              setCreating(false);
              pushToast({ kind: 'success', message: 'Volume create requested.' });
              refresh();
            }}
          />
        </Card>
      )}
    </div>
  );
}

function CreateVolumeForm({
  onCancel,
  onCreated,
}: {
  onCancel: () => void;
  onCreated: () => void;
}) {
  const pushToast = useToastStore((s) => s.push);
  const [name, setName] = useState('');
  const [driver, setDriver] = useState('local');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      await dockerApi.createVolume({ name, driver });
      onCreated();
    } catch (e) {
      pushToast({ kind: 'error', message: (e as Error).message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ padding: 18, display: 'flex', flexDirection: 'column' }}>
      <Label>Name</Label>
      <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="cooker-data" />
      <Label>Driver</Label>
      <Select value={driver} onChange={(e) => setDriver(e.target.value)}>
        <option value="local">local</option>
        <option value="nfs">nfs</option>
      </Select>
      <div style={{ display: 'flex', gap: 10, marginTop: 22, justifyContent: 'flex-end' }}>
        <Btn kind="ghost" onClick={onCancel} disabled={busy}>
          Cancel
        </Btn>
        <Btn kind="primary" onClick={submit} disabled={busy || !name}>
          {busy ? 'Creating…' : 'Create volume'}
        </Btn>
      </div>
    </div>
  );
}

function SectionHeader({
  title,
  count,
  action,
}: {
  title: string;
  count: number;
  action?: React.ReactNode;
}) {
  const t = useTheme();
  return (
    <div
      style={{
        padding: '14px 18px',
        borderBottom: `1px solid ${t.line}`,
        display: 'flex',
        alignItems: 'center',
        gap: 12,
      }}
    >
      <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
        {title}
      </span>
      <Pill>{count}</Pill>
      <div style={{ flex: 1 }} />
      {action}
    </div>
  );
}
