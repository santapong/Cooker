import { useCallback, useEffect, useState } from 'react';
import { useAuth } from '../../auth/OIDCProvider';
import { isNativeRuleFor, nativeAutomationApi, nativePreviewUsable, type NativeTriggerPreview, type NativeTriggerRule } from '../../api/nativeAutomation';
import type { Pipeline } from '../../types/pipeline';
import { Actions, Field, Select, TextInput } from '../ui/form';
import TriggerHistoryPanel from '../app/TriggerHistoryPanel';

const split = (value: string) => value.split(',').map((item) => item.trim()).filter(Boolean);
export default function NativeAutomationPanel({ pipeline, dirty }: { pipeline: Pipeline; dirty: boolean }) {
  const { user } = useAuth();
  const admin = !!user?.roles.includes('admin');
  const [rules, setRules] = useState<NativeTriggerRule[]>([]);
  const [editing, setEditing] = useState<NativeTriggerRule | null>(null);
  const initialKind = pipeline.source?.fullRef.startsWith('refs/tags/') ? 'tag' : 'branch';
  const [kind, setKind] = useState<'branch' | 'tag'>(initialKind);
  const [match, setMatch] = useState<'exact' | 'glob'>('exact');
  const [include, setInclude] = useState(pipeline.source?.fullRef.replace(/^refs\/(heads|tags)\//, '') ?? 'main');
  const [exclude, setExclude] = useState('');
  const [refName, setRefName] = useState(include);
  const [approval, setApproval] = useState<'review' | 'automatic'>('review');
  const [preview, setPreview] = useState<NativeTriggerPreview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const load = useCallback(async () => {
    if (!admin) return;
    const result = await nativeAutomationApi.rules();
    if (!Array.isArray(result)) throw new Error('Rule inventory unavailable');
    setRules(result.filter((rule): rule is NativeTriggerRule => isNativeRuleFor(rule, pipeline.id)));
  }, [admin, pipeline.id]);
  useEffect(() => {
    void load().catch(() => setError('Git rules could not be loaded. Check administrator access and retry.'));
  }, [load]);
  useEffect(() => { setPreview(null); }, [pipeline, dirty, kind, match, include, exclude, refName, approval, editing]);
  const execute = async (action: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try { await action(); }
    catch { setError('The source, grants or reviewed configuration could not be verified. Refresh and preview again before saving or evaluating a rule.'); }
    finally { setBusy(false); }
  };
  if (!admin) return <p>An administrator can configure exact branch or release-tag rules and inspect their delivery decisions.</p>;
  const fullRef = `refs/${kind === 'tag' ? 'tags' : 'heads'}/${refName}`;
  const draft = (): NativeTriggerRule => ({
    id: editing?.id ?? '', version: editing?.version ?? 0, name: `${pipeline.name} ${kind}`, enabled: true,
    appId: '', targetKind: 'pipeline', targetId: pipeline.id,
    source: { connectionId: pipeline.source?.connectionId ?? '' },
    refKind: kind, match, include: split(include), exclude: split(exclude), approval,
  });
  const canPrepare = !busy && !dirty && !!pipeline.source && !!include && !!refName;
  return <details className="pipeline-source-binding">
    <summary>Git deployment rules · {rules.length} {rules.length === 1 ? 'rule' : 'rules'}</summary>
    <p>A matching change runs this authored DAG at the verified commit. Review policy, saved pipeline version, environment versions and registry grants are checked before admission and execution.</p>
    {error && <p role="alert">{error}</p>}
    <Actions><button type="button" className="hud-btn" disabled={busy} onClick={() => void execute(load)}>Refresh Git rules</button></Actions>
    {rules.map((rule) => <article key={rule.id} className="section">
      <p><strong>{rule.name}</strong> · {rule.enabled ? rule.approval === 'automatic' ? 'Automatic within reviewed configuration' : 'Review each commit' : 'Disabled'}</p>
      <p>{rule.refKind} · {rule.include.join(', ')} · Reviewed pipeline version {rule.pipelineVersion ?? 'unavailable'}{rule.pipelineVersion !== pipeline.version && ' · Requires review of the current pipeline'}</p>
      <Actions><button type="button" className="hud-btn" disabled={busy} onClick={() => {
        setEditing(rule); setKind(rule.refKind); setMatch(rule.match); setInclude(rule.include.join(', ')); setExclude((rule.exclude ?? []).join(', ')); setApproval(rule.approval); setPreview(null);
      }}>Edit and preview rule</button>{rule.enabled && <button type="button" className="hud-btn" disabled={busy} onClick={() => void execute(async () => { await nativeAutomationApi.disable(rule.id); await load(); })}>Disable rule</button>}</Actions>
    </article>)}
    {!pipeline.source ? <p>Choose a connected source and save the pipeline before adding a Git rule.</p> : <>
      {dirty && <p role="status">Save your pipeline changes before previewing a rule. Existing rules do not approve an edited definition.</p>}
      <fieldset disabled={busy || dirty}>
        <Field label="Watch Git refs"><Select value={kind} onChange={(event) => setKind(event.target.value as 'branch' | 'tag')} options={[{ value: 'branch', label: 'Branch updates' }, { value: 'tag', label: 'Release tags' }]} /></Field>
        <Field label="Rule matching"><Select value={match} onChange={(event) => setMatch(event.target.value as 'exact' | 'glob')} options={[{ value: 'exact', label: 'Exact names' }, { value: 'glob', label: 'Name patterns' }]} /></Field>
        <Field label="Included names" hint="Comma-separated names without refs/heads/ or refs/tags/. Patterns such as release/* match one path segment."><TextInput value={include} onChange={(event) => setInclude(event.target.value)} /></Field>
        <Field label="Excluded names" hint="Optional. Exclusions take priority."><TextInput value={exclude} onChange={(event) => setExclude(event.target.value)} /></Field>
        <Field label="Git deployment approval"><Select value={approval} onChange={(event) => setApproval(event.target.value as 'review' | 'automatic')} options={[{ value: 'review', label: 'Review each commit' }, { value: 'automatic', label: 'Automatic within reviewed configuration' }]} /></Field>
        <Field label="Concrete ref to inspect" hint="A real matching branch or tag name is required even for a pattern rule."><TextInput value={refName} onChange={(event) => setRefName(event.target.value)} /></Field>
      </fieldset>
      <Actions><button type="button" className="hud-btn" disabled={!canPrepare} onClick={() => void execute(async () => {
        setPreview(null);
        const result = await nativeAutomationApi.preview(draft(), fullRef);
        if (!nativePreviewUsable(result, pipeline.id, pipeline.version)) throw new Error('Review evidence unavailable');
        setPreview(result);
      })}>Preview Git rule</button>{editing && <button type="button" className="hud-btn" disabled={busy} onClick={() => { setEditing(null); setPreview(null); }}>Draft new rule</button>}</Actions>
      {preview && <div className="section" aria-label="Native Git rule review">
        <p>Reviewed commit <code>{preview.envelope.commit}</code> · pipeline version {preview.rule.pipelineVersion ?? 'unavailable'}</p>
        <p>Changes to these source inputs require another configuration review: {preview.envelope.files.join(', ') || 'No files reported'}.</p>
        <button type="button" className="hud-btn hud-btn-primary" disabled={busy || dirty || !nativePreviewUsable(preview, pipeline.id, pipeline.version)} onClick={() => void execute(async () => { await nativeAutomationApi.save(preview, fullRef); setPreview(null); setEditing(null); await load(); })}>Approve configuration and save Git rule</button>
      </div>}
      {editing && <><p>Checking a missed change records a delivery decision. An enabled automatic rule may queue a deployment after its final checks.</p><button type="button" className="hud-btn" disabled={!canPrepare} onClick={() => void execute(async () => { await nativeAutomationApi.check(editing.id, fullRef); await load(); })}>Check ref and evaluate rule</button></>}
    </>}
    <TriggerHistoryPanel pipelineId={pipeline.id} sourceConnectionId={pipeline.source?.connectionId} />
  </details>;
}
