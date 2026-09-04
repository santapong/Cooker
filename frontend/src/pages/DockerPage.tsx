import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { dockerApi } from '../api/docker';
import { useDockerStore } from '../stores/dockerStore';
import type { DockerNetwork, DockerVolume } from '../types/infra';
import Badge from '../components/ui/Badge';
import Panel from '../components/ui/Panel';
import Gauge from '../components/instruments/Gauge';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Field, Select, TextInput } from '../components/ui/form';
import { ChartRows, type ChartRow } from '../components/list/StarChart';
import { pushToast } from '../stores/toastStore';
import { shortId, timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));
const mb = (bytes: number) => (bytes >= 1e9 ? `${(bytes / 1e9).toFixed(2)} GB` : `${Math.round(bytes / 1e6)} MB`);

/** Docker — instrument panels for the build host's images, containers, networks and volumes. */
export default function DockerPage() {
  const images = useDockerStore((s) => s.images);
  const containers = useDockerStore((s) => s.containers);
  const loading = useDockerStore((s) => s.loading);
  const storeError = useDockerStore((s) => s.error);
  const fetchImages = useDockerStore((s) => s.fetchImages);
  const fetchContainers = useDockerStore((s) => s.fetchContainers);
  const deleteImage = useDockerStore((s) => s.deleteImage);
  const stopContainer = useDockerStore((s) => s.stopContainer);
  const deleteContainer = useDockerStore((s) => s.deleteContainer);
  const [networks, setNetworks] = useState<DockerNetwork[]>([]);
  const [volumes, setVolumes] = useState<DockerVolume[]>([]);
  const [netForm, setNetForm] = useState({ name: '', driver: 'bridge' });
  const [volForm, setVolForm] = useState({ name: '', driver: 'local' });
  const [busy, setBusy] = useState<string | null>(null);

  const loadExtras = useCallback(async () => {
    const [n, v] = await Promise.all([dockerApi.listNetworks().catch(() => [] as DockerNetwork[]), dockerApi.listVolumes().catch(() => [] as DockerVolume[])]);
    setNetworks(n ?? []);
    setVolumes(v ?? []);
  }, []);
  useEffect(() => {
    void fetchImages();
    void fetchContainers();
    void loadExtras();
  }, [fetchImages, fetchContainers, loadExtras]);

  const act = async (key: string, fn: () => Promise<void>, ok: string) => {
    setBusy(key);
    try {
      await fn();
      pushToast('success', ok);
    } catch (e) {
      pushToast('error', message(e));
    } finally {
      setBusy(null);
    }
  };

  const running = containers.filter((c) => c.state === 'running').length;
  const imageRows: ChartRow[] = images.map((img) => ({
    id: img.id,
    name: img.repoTags?.[0] ?? shortId(img.id.replace('sha256:', '')),
    sub: img.repoTags?.length > 1 ? img.repoTags.slice(1).join(', ') : undefined,
    status: 'idle',
    meta: [mb(img.size), `${img.layers} layers`, timeAgo(img.created)],
    trailing: <ConfirmButton onConfirm={() => act(img.id, () => deleteImage(img.id), 'Image removed.')} disabled={busy !== null}>Remove</ConfirmButton>,
  }));
  const containerRows: ChartRow[] = containers.map((c) => ({
    id: c.id,
    name: c.name,
    sub: c.image,
    status: c.state === 'running' ? 'ok' : c.state === 'paused' ? 'warn' : 'idle',
    meta: [c.state, ...(c.ports?.length ? [c.ports.map((p) => `${p.hostPort}→${p.containerPort}/${p.protocol}`).join(' ')] : []), timeAgo(c.created)],
    trailing: (
      <>
        {c.state === 'running' && (
          <ConfirmButton className="hud-btn" confirmLabel="Stop?" onConfirm={() => act(c.id, () => stopContainer(c.id), 'Container stopped.')} disabled={busy !== null}>
            Stop
          </ConfirmButton>
        )}
        <ConfirmButton onConfirm={() => act(c.id, () => deleteContainer(c.id), 'Container removed.')} disabled={busy !== null}>Remove</ConfirmButton>
      </>
    ),
  }));
  const networkRows: ChartRow[] = networks.map((n) => ({
    id: n.id,
    name: n.name,
    status: 'idle',
    meta: [n.driver, n.scope],
    trailing: ['bridge', 'host', 'none'].includes(n.name) ? undefined : <ConfirmButton onConfirm={() => act(n.id, async () => { await dockerApi.deleteNetwork(n.id); await loadExtras(); }, 'Network removed.')}>Remove</ConfirmButton>,
  }));
  const volumeRows: ChartRow[] = volumes.map((v) => ({
    id: v.name,
    name: v.name,
    sub: v.mountpoint,
    status: 'idle',
    meta: [v.driver],
    trailing: <ConfirmButton onConfirm={() => act(v.name, async () => { await dockerApi.deleteVolume(v.name); await loadExtras(); }, 'Volume removed.')}>Remove</ConfirmButton>,
  }));

  const createNetwork = (e: FormEvent) => {
    e.preventDefault();
    void act('net', async () => { await dockerApi.createNetwork({ name: netForm.name.trim(), driver: netForm.driver }); setNetForm({ name: '', driver: 'bridge' }); await loadExtras(); }, 'Network created.');
  };
  const createVolume = (e: FormEvent) => {
    e.preventDefault();
    void act('vol', async () => { await dockerApi.createVolume({ name: volForm.name.trim(), driver: volForm.driver }); setVolForm({ name: '', driver: 'local' }); await loadExtras(); }, 'Volume created.');
  };

  return (
    <div className="detail">
      <header className="detail-head">
        <div className="grow">
          <h1>Docker</h1>
          <p>Images cached on the build host, its containers, networks and volumes.</p>
        </div>
        <Actions>
          <button type="button" className="hud-btn" onClick={() => { void fetchImages(); void fetchContainers(); void loadExtras(); }} disabled={loading}>
            {loading ? 'Refreshing…' : 'Refresh'}
          </button>
        </Actions>
      </header>
      {storeError && (
        <div className="form-error" role="alert">
          {storeError}
        </div>
      )}
      <div className="gauges">
        <Gauge label="Images" value={images.length} sub={`${mb(images.reduce((a, i) => a + (i.size || 0), 0))} on disk`} />
        <Gauge label="Containers" value={running} sub={`of ${containers.length} running`} tone={running ? 'ok' : 'default'} />
        <Gauge label="Networks" value={networks.length} />
        <Gauge label="Volumes" value={volumes.length} />
      </div>
      <div className="panel-grid">
        <Panel title="Images" aside={`${images.length}`} className="panel-span">
          {imageRows.length ? <ChartRows rows={imageRows} hasThumbs={false} /> : <p>{loading ? 'Loading…' : 'No images on the build host.'}</p>}
        </Panel>
        <Panel title="Containers" aside={`${running} running`} className="panel-span">
          {containerRows.length ? <ChartRows rows={containerRows} hasThumbs={false} /> : <p>{loading ? 'Loading…' : 'No containers.'}</p>}
        </Panel>
        <Panel title="Networks" aside={`${networks.length}`}>
          {networkRows.length ? <ChartRows rows={networkRows} hasThumbs={false} /> : <p>No networks reported.</p>}
          <form className="form-actions" onSubmit={createNetwork}>
            <Field label="Name">
              <TextInput value={netForm.name} onChange={(e) => setNetForm({ ...netForm, name: e.target.value })} placeholder="cooker-proxy" required />
            </Field>
            <Field label="Driver">
              <Select value={netForm.driver} onChange={(e) => setNetForm({ ...netForm, driver: e.target.value })} options={['bridge', 'overlay', 'macvlan'].map((d) => ({ value: d, label: d }))} />
            </Field>
            <button type="submit" className="hud-btn" disabled={busy !== null || !netForm.name.trim()} style={{ alignSelf: 'flex-end' }}>
              ＋ Network
            </button>
          </form>
        </Panel>
        <Panel title="Volumes" aside={`${volumes.length}`}>
          {volumeRows.length ? <ChartRows rows={volumeRows} hasThumbs={false} /> : <p>No volumes reported.</p>}
          <form className="form-actions" onSubmit={createVolume}>
            <Field label="Name">
              <TextInput value={volForm.name} onChange={(e) => setVolForm({ ...volForm, name: e.target.value })} placeholder="pgdata" required />
            </Field>
            <Field label="Driver">
              <Select value={volForm.driver} onChange={(e) => setVolForm({ ...volForm, driver: e.target.value })} options={[{ value: 'local', label: 'local' }]} />
            </Field>
            <button type="submit" className="hud-btn" disabled={busy !== null || !volForm.name.trim()} style={{ alignSelf: 'flex-end' }}>
              ＋ Volume
            </button>
          </form>
        </Panel>
      </div>
      <Badge variant="muted">{containers.length + images.length === 0 && !loading ? 'the dev image has no docker.sock — panels read empty' : 'live'}</Badge>
    </div>
  );
}
