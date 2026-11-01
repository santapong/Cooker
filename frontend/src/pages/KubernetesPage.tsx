import { useCallback, useEffect, useState } from 'react';
import { useKubernetesStore } from '../stores/kubernetesStore';
import { useKubeWatch } from '../hooks/useKubeWatch';
import Panel from '../components/ui/Panel';
import Gauge from '../components/instruments/Gauge';
import ConfirmButton from '../components/ui/ConfirmButton';
import { Actions, Field, Select, TextInput } from '../components/ui/form';
import { ChartRows, type ChartRow } from '../components/list/StarChart';
import { pushToast } from '../stores/toastStore';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Kubernetes — workloads per namespace, live via the watch socket, with scale and restart. */
export default function KubernetesPage() {
  const namespaces = useKubernetesStore((s) => s.namespaces);
  const workloads = useKubernetesStore((s) => s.workloads);
  const selected = useKubernetesStore((s) => s.selectedNamespace);
  const loading = useKubernetesStore((s) => s.loading);
  const error = useKubernetesStore((s) => s.error);
  const unavailable = useKubernetesStore((s) => s.clusterUnavailable);
  const fetchNamespaces = useKubernetesStore((s) => s.fetchNamespaces);
  const fetchWorkloads = useKubernetesStore((s) => s.fetchWorkloads);
  const setNamespace = useKubernetesStore((s) => s.setNamespace);
  const scale = useKubernetesStore((s) => s.scale);
  const restart = useKubernetesStore((s) => s.restart);
  const [replicas, setReplicas] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);

  useEffect(() => {
    void fetchNamespaces();
  }, [fetchNamespaces]);
  useEffect(() => {
    if (!unavailable) void fetchWorkloads(selected);
  }, [selected, unavailable, fetchWorkloads]);

  const onEvent = useCallback(() => {
    void fetchWorkloads(selected);
  }, [fetchWorkloads, selected]);
  useKubeWatch(selected, 'deployments', unavailable ? undefined : onEvent);

  const act = async (key: string, fn: () => Promise<void>, ok: string) => {
    setBusy(key);
    try {
      await fn();
      pushToast('success', ok);
      await fetchWorkloads(selected);
    } catch (e) {
      pushToast('error', message(e));
    } finally {
      setBusy(null);
    }
  };

  const ready = workloads.reduce((a, w) => a + (w.ready || 0), 0);
  const desired = workloads.reduce((a, w) => a + (w.replicas || 0), 0);
  const rows: ChartRow[] = workloads.map((w) => {
    const key = `${w.kind}/${w.name}`;
    return {
      id: key,
      name: w.name,
      sub: w.image,
      status: w.replicas > 0 && w.ready >= w.replicas ? 'ok' : w.ready > 0 ? 'warn' : 'idle',
      meta: [w.kind, `${w.ready}/${w.replicas} ready`, w.status],
      trailing: (
        <>
          <TextInput type="number" min={0} style={{ width: 64, height: 26 }} value={replicas[key] ?? String(w.replicas)} onChange={(e) => setReplicas({ ...replicas, [key]: e.target.value })} aria-label={`Replicas for ${w.name}`} />
          <button type="button" className="hud-btn" disabled={busy !== null} onClick={() => act(key, () => scale(w.namespace, w.kind, w.name, Number(replicas[key] ?? w.replicas)), `${w.name} scaled.`)}>
            Scale
          </button>
          <ConfirmButton className="hud-btn" confirmLabel="Restart?" disabled={busy !== null} onConfirm={() => act(key, () => restart(w.namespace, w.kind, w.name), `${w.name} restarting.`)}>
            Restart
          </ConfirmButton>
        </>
      ),
    };
  });

  return (
    <div className="detail">
      <header className="detail-head">
        <div className="grow">
          <h1>Kubernetes</h1>
          <p>Deployments, StatefulSets and DaemonSets in the namespaces this cluster exposes.</p>
        </div>
        <Actions>
          <Field label="Namespace">
            <Select value={selected} onChange={(e) => setNamespace(e.target.value)} disabled={unavailable} options={(namespaces.length ? namespaces.map((n) => n.name) : [selected]).map((n) => ({ value: n, label: n }))} />
          </Field>
          <button type="button" className="hud-btn" onClick={() => { void fetchNamespaces(); void fetchWorkloads(selected); }} disabled={loading} style={{ alignSelf: 'flex-end' }}>
            {loading ? 'Refreshing…' : 'Refresh'}
          </button>
        </Actions>
      </header>
      {unavailable ? (
        <Panel title="Cluster not reachable">
          <p>{error ?? 'No cluster is configured.'}</p>
          <p>Set COOKER_KUBECONFIG (or run in-cluster) and add the cluster under Settings → Kubernetes clusters.</p>
        </Panel>
      ) : (
        <>
          {error && (
            <div className="form-error" role="alert">
              {error}
            </div>
          )}
          <div className="gauges">
            <Gauge label="Namespaces" value={namespaces.length} />
            <Gauge label="Workloads" value={workloads.length} sub={selected} />
            <Gauge label="Ready" value={desired ? `${Math.round((ready / desired) * 100)}%` : '—'} sub={`${ready} of ${desired} replicas`} tone={desired && ready >= desired ? 'ok' : desired ? 'fail' : 'default'} />
          </div>
          <Panel title="Workloads" aside={selected}>
            {rows.length ? <ChartRows rows={rows} hasThumbs={false} /> : <p>{loading ? 'Loading…' : `No workloads in ${selected}.`}</p>}
          </Panel>
        </>
      )}
    </div>
  );
}
