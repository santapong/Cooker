import { useCallback, useEffect, useState } from 'react';
import { get } from '../../api/client';
import type { PipelineSourceConfig } from '../../types/pipeline';
import { Field, Select, TextInput } from '../ui/form';
import { sourceBindingIssue, type SourceChoice } from './sourceBinding';

export default function PipelineSourceBinding({ value, onChange, disabled }: {
  value?: PipelineSourceConfig | null;
  onChange: (source: PipelineSourceConfig | null) => void;
  disabled: boolean;
}) {
  const [connections, setConnections] = useState<SourceChoice[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    setError(null);
    try {
      const result = await get<SourceChoice[]>('/apps/source-connections');
      if (!signal?.aborted) setConnections(result ?? []);
    } catch {
      if (!signal?.aborted) setError('Ask an administrator to check connection access, then retry.');
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);
  const connection = connections.find((item) => item.id === value?.connectionId);
  const issue = !loading && !error ? sourceBindingIssue(value, connections) : null;
  const controlsDisabled = disabled || loading || !!error;
  return <details className="pipeline-source-binding" open={!!value}>
    <summary>Source · {value ? `${value.repository || 'Choose repository'} @ ${value.fullRef || 'Choose ref'}` : 'Host build context'}</summary>
    <p>A connected source is resolved to an exact commit when a run is accepted. Checkout is verified by the worker before a build starts. Save changes before creating or reviewing Git rules.</p>
    {loading && <p role="status">Loading approved source connections…</p>}
    {error && <div role="alert"><p>Source connections could not be loaded: {error}</p><button type="button" className="hud-btn" onClick={() => void load()}>Retry</button></div>}
    <fieldset disabled={controlsDisabled}>
      <Field label="Build source"><Select value={value ? 'connected' : 'host'} onChange={(event) => {
        if (disabled) return;
        const first = connections.find((item) => item.enabled && item.repositories.length);
        onChange(event.target.value === 'host' ? null : { connectionId: first?.id ?? '', repository: first?.repositories[0] ?? '', fullRef: 'refs/heads/main' });
      }} options={[{ value: 'host', label: 'Existing host build context' }, { value: 'connected', label: 'Connected Git repository' }]} /></Field>
      {value && <div className="pipeline-source-fields">
        <Field label="Source connection"><Select value={value.connectionId} onChange={(event) => {
          const next = connections.find((item) => item.id === event.target.value);
          onChange({ ...value, connectionId: event.target.value, repository: next?.repositories[0] ?? '' });
        }} options={[
          { value: '', label: 'Choose a connection…' },
          ...(!connection && value.connectionId ? [{ value: value.connectionId, label: `${value.connectionId} · unavailable` }] : []),
          ...connections.filter((item) => item.enabled).map((item) => ({ value: item.id, label: `${item.name} · ${item.provider}` })),
        ]} /></Field>
        <Field label="Repository"><Select value={value.repository} onChange={(event) => onChange({ ...value, repository: event.target.value })} options={[
          { value: '', label: 'Choose a repository…' },
          ...(value.repository && !connection?.repositories.includes(value.repository) ? [{ value: value.repository, label: `${value.repository} · unavailable` }] : []),
          ...(connection?.repositories ?? []).map((repository) => ({ value: repository, label: repository })),
        ]} /></Field>
        <Field label="Full branch or tag ref" hint="Examples: refs/heads/main · refs/tags/v1.0.0"><TextInput value={value.fullRef} onChange={(event) => onChange({ ...value, fullRef: event.target.value })} placeholder="refs/heads/main" /></Field>
      </div>}
    </fieldset>
    {issue && <p role="alert">{issue}</p>}
    {!loading && !error && !connections.length && <p>No enabled source connections are available. An administrator can add GitHub, GitLab or Gitea in Settings → Git connections.</p>}
    {!value && <p>Host context mode uses the existing build paths. It has no connected Git source receipt.</p>}
  </details>;
}
