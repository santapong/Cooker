import { useEffect, useState } from 'react';
import { nativeRuntimeApi } from '../../api/nativeRuntime';
import type { NativeRuntimeStatus, NativeRuntimeSubject, RuntimeReceipt } from '../../types/nativeRuntime';
import { nativeStatusMatches } from '../../utils/nativeRuntime';
import Caps from '../ui/Caps';
import NativeStageLogInspector from './NativeStageLogInspector';

const healthLabels: Record<NativeRuntimeStatus['health'], string> = {
  healthy: 'Healthy', unhealthy: 'Unhealthy', starting: 'Starting', not_configured: 'No health check configured', unknown: 'Not known',
};

/** Observes one retained deployment; the current graph never selects its container. */
export default function NativeRuntimePanel({ subject, receipt, onStatus }: { subject: NativeRuntimeSubject; receipt?: RuntimeReceipt; onStatus?: (status: NativeRuntimeStatus | null) => void }) {
  const [status, setStatus] = useState<NativeRuntimeStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const { pipelineId, runId, stageId } = subject;
  const { containerId, name, imageId, imageReference } = receipt ?? {};
  useEffect(() => {
    let cancelled = false;
    const target: NativeRuntimeSubject = { kind: 'native-stage', pipelineId, runId, stageId };
    setStatus(null);
    setLoading(true);
    setError(false);
    onStatus?.(null);
    nativeRuntimeApi.status(target).then((result) => {
      if (cancelled) return;
      const matchesReceipt = !containerId || result.binding === 'unavailable' || (result.containerId === containerId && result.name === name && result.imageId === imageId && result.imageReference === imageReference);
      if (!nativeStatusMatches(result, target) || !matchesReceipt) { setError(true); return; }
      setStatus(result);
      onStatus?.(result);
    }).catch(() => { if (!cancelled) setError(true); }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; onStatus?.(null); };
  }, [pipelineId, runId, stageId, containerId, name, imageId, imageReference, refresh, onStatus]);

  return <section className="field" aria-label="Recorded deployment runtime">
    <Caps>Recorded deployment runtime</Caps>
    <p>This observation belongs to this run’s recorded container. A successful deployment step does not confirm its current health.</p>
    {loading && <p role="status">Checking the recorded container…</p>}
    {error && <p role="alert">The recorded runtime could not be verified. Refresh to check again.</p>}
    {status?.binding === 'unavailable' && <p role="status">Runtime inspection is unavailable for this recorded deployment. No replacement container has been selected.</p>}
    {status?.binding === 'verified' && <>
      <div className="kv"><Caps>Deployment step</Caps><span className="v">{status.recordedStageStatus || 'unknown'}</span><Caps>Container state</Caps><span className="v">{status.state}</span><Caps>Health check</Caps><span className="v">{healthLabels[status.health] ?? 'Not known'}</span><Caps>Observed</Caps><span className="v">{status.observedAt ? new Date(status.observedAt).toLocaleString() : 'unavailable'}</span></div>
      <details><summary>Recorded container identity</summary><div className="kv"><Caps>Container</Caps><span className="v mono">{status.containerId}</span><Caps>Name</Caps><span className="v">{status.name}</span><Caps>Image</Caps><span className="v mono">{status.imageReference}</span><Caps>Image ID</Caps><span className="v mono">{status.imageId}</span></div></details>
      {status.containerId && <NativeStageLogInspector subject={subject} containerId={status.containerId} />}
    </>}
    <button type="button" className="hud-btn" disabled={loading} onClick={() => setRefresh((value) => value + 1)}>Refresh runtime</button>
  </section>;
}
