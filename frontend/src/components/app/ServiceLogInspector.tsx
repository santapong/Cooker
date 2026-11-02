import { useCallback, useEffect, useId, useMemo, useRef, useState, type FormEvent } from 'react';
import { useAuth } from '../../auth/OIDCProvider';
import { ApiError } from '../../api/client';
import { runtimeLogsApi } from '../../api/runtimeLogs';
import { nativeRuntimeApi } from '../../api/nativeRuntime';
import type { NativeRuntimeLogPage, NativeRuntimeLogReplicas, NativeRuntimeSubject } from '../../types/nativeRuntime';
import type { RuntimeLogPage, RuntimeLogReplicas } from '../../types/runtimeLogs';
import { downloadText } from '../../utils/download';
import { logPageMatches, runtimeLogError, runtimeLogOptions, type LogRange } from '../../utils/runtimeLogs';
import { nativeLogPageMatches, nativeSubjectMatches } from '../../utils/nativeRuntime';
import './service-logs.css';

export default function ServiceLogInspector({ appId, service }: { appId: string; service: string }) {
  const { user } = useAuth();
  const [open, setOpen] = useState(false);
  const id = useId();
  const canRead = !!user?.roles.some((role) => role === 'operator' || role === 'admin');
  if (!canRead) return <p className="workspace-note">Runtime logs require an operator or administrator role.</p>;
  return <div className="service-log-inspector">
    <button type="button" className="hud-btn" aria-expanded={open} aria-controls={id} onClick={() => setOpen((value) => !value)}>{open ? 'Close log browser' : 'Browse current Docker logs'}</button>
    {open && <div id={id}><RuntimeLogBrowser key={`${appId}:${service}`} appId={appId} service={service} /></div>}
  </div>;
}

type LogPage = RuntimeLogPage | NativeRuntimeLogPage;
type LogReplicas = RuntimeLogReplicas | NativeRuntimeLogReplicas;
type LogTarget = { appId: string; service: string; subject?: never; expectedContainerId?: never } | { subject: NativeRuntimeSubject; expectedContainerId: string; appId?: never; service?: never };

/** Both targets share the browser controls, while their server identities stay distinct. */
export function RuntimeLogBrowser({ appId = '', service = '', subject, expectedContainerId }: LogTarget) {
  const id = useId();
  const [replicas, setReplicas] = useState<LogReplicas | null>(null);
  const [containerId, setContainerId] = useState('');
  const [range, setRange] = useState<LogRange>('1h');
  const [from, setFrom] = useState('');
  const [until, setUntil] = useState('');
  const [search, setSearch] = useState('');
  const [maxLines, setMaxLines] = useState(2000);
  const [snapshot, setSnapshot] = useState<LogPage | null>(null);
  const [busy, setBusy] = useState<string | null>('replicas');
  const [error, setError] = useState<string | null>(null);
  const sequence = useRef(0);
  const currentSession = useRef<string | null>(null);
  const pipelineId = subject?.pipelineId ?? '', runId = subject?.runId ?? '', stageId = subject?.stageId ?? '';
  const nativeTarget = useMemo((): NativeRuntimeSubject | null => pipelineId && runId && stageId ? { kind: 'native-stage', pipelineId, runId, stageId } : null, [pipelineId, runId, stageId]);
  const label = nativeTarget?.stageId ?? service;
  const api = useMemo(() => nativeTarget ? {
    replicas: () => nativeRuntimeApi.replicas(nativeTarget),
    open: (options: Parameters<typeof nativeRuntimeApi.open>[1]) => nativeRuntimeApi.open(nativeTarget, options),
    page: (id: string, offset: number) => nativeRuntimeApi.page(nativeTarget, id, offset),
    download: (id: string) => nativeRuntimeApi.download(nativeTarget, id),
    close: (id: string) => nativeRuntimeApi.close(nativeTarget, id),
  } : {
    replicas: () => runtimeLogsApi.replicas(appId, service),
    open: (options: Parameters<typeof runtimeLogsApi.open>[2]) => runtimeLogsApi.open(appId, service, options),
    page: (id: string, offset: number) => runtimeLogsApi.page(appId, service, id, offset),
    download: (id: string) => runtimeLogsApi.download(appId, service, id),
    close: (id: string) => runtimeLogsApi.close(appId, service, id),
  }, [nativeTarget, appId, service]);
  const pageMatches = (page: LogPage, expected: { runId: string; containerId: string; sessionId?: string; offset?: number }) => nativeTarget
    ? nativeLogPageMatches(page as NativeRuntimeLogPage, { ...expected, subject: nativeTarget })
    : !('subject' in page) && logPageMatches(page as RuntimeLogPage, { ...expected, appId, service });
  const closeQuietly = useCallback((sessionId: string | null) => {
    if (sessionId) void api.close(sessionId).catch(() => undefined);
  }, [api]);

  const refresh = useCallback(async () => {
    const version = ++sequence.current;
    closeQuietly(currentSession.current);
    currentSession.current = null;
    setSnapshot(null);
    setReplicas(null);
    setContainerId('');
    setBusy('replicas');
    setError(null);
    try {
      const result = await api.replicas();
      if (version !== sequence.current) return;
      if (nativeTarget ? !('subject' in result) || !nativeSubjectMatches(result.subject, nativeTarget) || result.runId !== nativeTarget.runId || result.replicas.length !== 1 || result.replicas[0].containerId !== expectedContainerId : !('appId' in result) || result.appId !== appId || result.service !== service) throw new Error('Mismatched runtime identity');
      setReplicas(result);
      setContainerId(result.replicas.length === 1 ? result.replicas[0].containerId : '');
    } catch (failure) {
      if (version === sequence.current) setError(runtimeLogError(failure));
    } finally {
      if (version === sequence.current) setBusy(null);
    }
  }, [api, nativeTarget, appId, service, expectedContainerId, closeQuietly]);

  useEffect(() => {
    void refresh();
    return () => {
      sequence.current++;
      closeQuietly(currentSession.current);
      currentSession.current = null;
    };
  }, [refresh, closeQuietly]);

  const sessionId = snapshot?.sessionId;
  const expiresAt = snapshot?.expiresAt;
  useEffect(() => {
    if (!sessionId || !expiresAt) return;
    const remaining = Date.parse(expiresAt) - Date.now();
    const timer = window.setTimeout(() => {
      sequence.current++;
      currentSession.current = null;
      setSnapshot(null);
      setBusy(null);
      setError('This snapshot expired. Capture a new snapshot to read logs.');
    }, Math.max(0, remaining));
    return () => window.clearTimeout(timer);
  }, [sessionId, expiresAt]); // expiry is unchanged by paging

  const capture = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || !replicas) return;
    let options;
    try { options = runtimeLogOptions({ containerId, runId: replicas.runId, range, from, until, search, maxLines }); }
    catch (failure) { setError(failure instanceof Error ? failure.message : 'Check the log options.'); return; }
    const version = ++sequence.current;
    closeQuietly(currentSession.current);
    currentSession.current = null;
    setSnapshot(null);
    setBusy('capture');
    setError(null);
    try {
      const page = await api.open(options);
      if (version !== sequence.current) { closeQuietly(page.sessionId); return; }
      if (!pageMatches(page, { runId: replicas.runId, containerId, offset: 0 })) { closeQuietly(page.sessionId); throw new Error('Mismatched snapshot identity'); }
      currentSession.current = page.sessionId;
      setSnapshot(page);
    } catch (failure) {
      if (version === sequence.current) setError(runtimeLogError(failure));
    } finally {
      if (version === sequence.current) setBusy(null);
    }
  };

  const reportReadError = (failure: unknown) => {
    if (failure instanceof ApiError && [403, 404, 503].includes(failure.status)) {
      closeQuietly(currentSession.current);
      currentSession.current = null;
      setSnapshot(null);
    }
    setError(runtimeLogError(failure));
  };
  const readPage = async (offset: number) => {
    if (!snapshot || busy) return;
    const version = ++sequence.current;
    setBusy('page');
    setError(null);
    try {
      const page = await api.page(snapshot.sessionId, offset);
      if (version !== sequence.current) return;
      if (!pageMatches(page, { ...snapshot, offset })) throw new Error('Mismatched snapshot identity');
      setSnapshot(page);
    } catch (failure) { if (version === sequence.current) reportReadError(failure); }
    finally { if (version === sequence.current) setBusy(null); }
  };
  const download = async () => {
    if (!snapshot || busy) return;
    const version = ++sequence.current;
    setBusy('download');
    setError(null);
    try {
      const text = await api.download(snapshot.sessionId);
      if (version === sequence.current) downloadText(nativeTarget ? 'cooker-stage-logs.txt' : 'cooker-service-logs.txt', text, 'text/plain');
    } catch (failure) { if (version === sequence.current) reportReadError(failure); }
    finally { if (version === sequence.current) setBusy(null); }
  };

  return <section className="service-log-browser" aria-label={`${label} Docker log browser`}>
    <div className="workspace-section-head"><h5>{nativeTarget ? 'Recorded stage log snapshot' : 'Service log snapshot'}</h5><button type="button" className="hud-btn" onClick={() => void refresh()} disabled={!!busy}>{nativeTarget ? 'Refresh recorded container' : 'Refresh replicas'}</button></div>
    <p className="workspace-note">{nativeTarget ? 'Capture logs from this run’s exact recorded container.' : 'Capture logs from the current recorded deployment.'} A snapshot stays on one container; it does not follow new output or replacement replicas.</p>
    {error && <p role="alert">{error}</p>}
    {busy && <p role="status">{busy === 'replicas' ? 'Finding recorded replicas…' : busy === 'capture' ? 'Capturing redacted logs…' : busy === 'download' ? 'Preparing download…' : 'Reading the same snapshot…'}</p>}
    {replicas && <form onSubmit={(event) => void capture(event)}>
      <fieldset disabled={!!busy} className="service-log-fields">
        <legend className="sr-only">Snapshot options</legend>
        <label htmlFor={`${id}-replica`}>{nativeTarget ? 'Recorded container' : 'Replica'}<select id={`${id}-replica`} className="input" value={containerId} onChange={(event) => setContainerId(event.target.value)} required><option value="">Choose a replica</option>{replicas.replicas.map((replica) => <option key={replica.containerId} value={replica.containerId}>{replica.name} · {replica.state}</option>)}</select></label>
        <label htmlFor={`${id}-range`}>Time range<select id={`${id}-range`} className="input" value={range} onChange={(event) => setRange(event.target.value as LogRange)}><option value="1h">Last hour</option><option value="15m">Last 15 minutes</option><option value="6h">Last 6 hours</option><option value="24h">Last 24 hours</option><option value="custom">Custom range</option></select></label>
        {range === 'custom' && <><label htmlFor={`${id}-from`}>From (local time)<input id={`${id}-from`} className="input" type="datetime-local" value={from} onChange={(event) => setFrom(event.target.value)} required /></label><label htmlFor={`${id}-until`}>Until (local time)<input id={`${id}-until`} className="input" type="datetime-local" value={until} onChange={(event) => setUntil(event.target.value)} required /></label></>}
        <label htmlFor={`${id}-search`}>Search redacted text<input id={`${id}-search`} className="input" value={search} maxLength={256} onChange={(event) => setSearch(event.target.value)} placeholder="Literal text, optional" /></label>
        <label htmlFor={`${id}-limit`}>Tail limit<select id={`${id}-limit`} className="input" value={maxLines} onChange={(event) => setMaxLines(Number(event.target.value))}><option value={2000}>2,000 lines</option><option value={5000}>5,000 lines</option><option value={10000}>10,000 lines</option></select></label>
        <button type="submit" className="hud-btn hud-btn-primary" disabled={!containerId}>Capture snapshot</button>
      </fieldset>
      <p className="workspace-note">The selected tail is bounded to 1 MiB. Search applies after known secrets are redacted. Snapshots expire after 10 minutes or a Cooker restart; older Docker logs may already be unavailable.</p>
    </form>}
    {snapshot && <div className="service-log-snapshot">
      <details className="service-log-identity"><summary>Captured deployment and container</summary><dl><dt>Run</dt><dd>{snapshot.runId}</dd><dt>Container</dt><dd>{snapshot.containerId}</dd><dt>Range</dt><dd>{new Date(snapshot.since).toLocaleString()} → {new Date(snapshot.until).toLocaleString()}</dd><dt>Expires</dt><dd>{new Date(snapshot.expiresAt).toLocaleString()}</dd></dl></details>
      <p className="workspace-note">{snapshot.coverage}</p>
      {snapshot.tailLimitReached && <p role="status">The tail limit was reached. Earlier lines in this range may be missing.</p>}
      <div className="service-log-output" tabIndex={0} aria-label={`${label} captured log output`}>
        {snapshot.entries.length ? <pre>{snapshot.entries.map((entry) => `${entry.sequence}  ${entry.timestamp ? entry.timestamp + '  ' : ''}${entry.message}`).join('\n')}</pre> : <p>No lines match this snapshot and search.</p>}
      </div>
      <div className="service-log-paging"><span>{snapshot.entries.length ? `${snapshot.offset + 1}–${snapshot.offset + snapshot.entries.length}` : '0'} of {snapshot.total} matching lines</span><button type="button" className="hud-btn" disabled={!!busy || snapshot.offset === 0} onClick={() => void readPage(Math.max(0, snapshot.offset - 200))}>Previous lines</button><button type="button" className="hud-btn" disabled={!!busy || snapshot.nextOffset === undefined} onClick={() => void readPage(snapshot.nextOffset ?? 0)}>Next lines</button><button type="button" className="hud-btn" disabled={!!busy} onClick={() => void download()}>Download redacted logs</button></div>
    </div>}
  </section>;
}
