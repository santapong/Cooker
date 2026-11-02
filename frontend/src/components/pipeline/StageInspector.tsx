import { useEffect, useRef, useState, type ReactNode } from 'react';
import { usePipelineStore } from '../../stores/pipelineStore';
import type { Stage, StageConfig } from '../../types/pipeline';
import Badge from '../ui/Badge';
import Caps from '../ui/Caps';
import StageSymbol from './StageSymbol';
import StageEnvironmentBindings from './StageEnvironmentBindings';
import RegistrySelection from '../registry/RegistrySelection';
import { dependencyStory, requirementSummary } from './dependencies';

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="field">
      <Caps>{label}</Caps>
      {children}
    </label>
  );
}

function Text({
  value,
  onChange,
  placeholder,
  mono = true,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  mono?: boolean;
}) {
  return (
    <input
      className="input"
      style={mono ? undefined : { fontFamily: 'var(--font-body)' }}
      value={value}
      placeholder={placeholder}
      onChange={(e) => onChange(e.target.value)}
      spellCheck={false}
    />
  );
}

const listToText = (v?: string[]) => (v ?? []).join(', ');
const textToList = (v: string) => v.split(',').map((s) => s.trim()).filter(Boolean);
const argsToText = (v?: string[]) => (v ?? []).join(' ');
const textToArgs = (v: string) => v.split(/\s+/).filter(Boolean);

function TypeFields({ stage, set }: { stage: Stage; set: (c: Partial<StageConfig>) => void }) {
  const c = stage.config;
  switch (stage.type) {
    case 'build':
      return (
        <>
          <Field label="Dockerfile"><Text value={c.dockerfile ?? ''} onChange={(v) => set({ dockerfile: v })} placeholder="Dockerfile" /></Field>
          <Field label="Context"><Text value={c.context ?? ''} onChange={(v) => set({ context: v })} placeholder="." /></Field>
          <Field label="Tags"><Text value={listToText(c.tags)} onChange={(v) => set({ tags: textToList(v) })} placeholder="ghcr.io/org/app:latest, ghcr.io/org/app:sha" /></Field>
          <Field label="Platforms"><Text value={listToText(c.platforms)} onChange={(v) => set({ platforms: textToList(v) })} placeholder="linux/amd64, linux/arm64" /></Field>
        </>
      );
    case 'test':
      return (
        <>
          <Field label="Image"><Text value={c.image ?? ''} onChange={(v) => set({ image: v })} placeholder="node:20-alpine" /></Field>
          <Field label="Command"><Text value={argsToText(c.command)} onChange={(v) => set({ command: textToArgs(v) })} placeholder="npm test" /></Field>
        </>
      );
    case 'push':
      return (
        <>
          <Field label="Registry"><Text value={c.registry ?? ''} onChange={(v) => set({ registry: v })} placeholder="ghcr.io" /></Field>
          <Field label="Repository"><Text value={c.repository ?? ''} onChange={(v) => set({ repository: v })} placeholder="org/image" /></Field>
        </>
      );
    case 'deploy':
      return (
        <>
          <Field label="Runtime"><select className="input" value={c.deployRuntime ?? ''} onChange={(event) => set({ deployRuntime: event.target.value ? event.target.value as StageConfig['deployRuntime'] : undefined })}>
            <option value="">Existing manifest / Helm dispatch</option>
            <option value="docker">Docker container</option>
            <option value="compose">Docker Compose · host context</option>
            {c.deployRuntime && !['docker', 'compose'].includes(c.deployRuntime) && <option value={c.deployRuntime}>{c.deployRuntime} · existing configuration</option>}
          </select></Field>
          {(c.deployRuntime === 'docker' || c.deployRuntime === 'compose') ? <>
            <Field label="Image"><Text value={c.image ?? ''} onChange={(v) => set({ image: v })} placeholder="${stages.push.image}" /></Field>
            <Field label="Runtime name"><Text value={c.runtimeName ?? ''} onChange={(v) => set({ runtimeName: v })} placeholder="app-web" /></Field>
            {c.deployRuntime === 'docker' && <Field label="Published ports"><Text value={listToText(c.composePorts)} onChange={(v) => set({ composePorts: textToList(v) })} placeholder="8080:80" /></Field>}
            {c.deployRuntime === 'compose' && <>
              <Field label="Compose file"><Text value={c.manifestPath ?? ''} onChange={(v) => set({ manifestPath: v })} placeholder="compose.yaml" /></Field>
              <Field label="Compose project"><Text value={c.composeProject ?? ''} onChange={(v) => set({ composeProject: v })} placeholder="app" /></Field>
            </>}
          </> : <>
          <Field label="Namespace"><Text value={c.namespace ?? ''} onChange={(v) => set({ namespace: v })} placeholder="default" /></Field>
          <Field label="Manifest path"><Text value={c.manifestPath ?? ''} onChange={(v) => set({ manifestPath: v })} placeholder="k8s/deployment.yaml" /></Field>
          <Field label="Helm chart"><Text value={c.helmChart ?? ''} onChange={(v) => set({ helmChart: v })} placeholder="oci://ghcr.io/org/chart" /></Field>
          </>}
          <p>Connected Git deployment supports Docker containers in this release. Scoped registry execution supports Docker and host-context Compose; other transports require a later adapter.</p>
        </>
      );
    case 'custom':
      return (
        <>
          <Field label="Image"><Text value={c.image ?? ''} onChange={(v) => set({ image: v })} placeholder="alpine:3.20" /></Field>
          <Field label="Script">
            <textarea
              className="input"
              value={c.script ?? ''}
              placeholder="echo hello"
              spellCheck={false}
              onChange={(e) => set({ script: e.target.value })}
            />
          </Field>
        </>
      );
    case 'approval':
      return <p>Manual gate. The run pauses here until an approver signs off on the run page.</p>;
    default:
      return null;
  }
}

/** Plain-language requirements first; editable configuration stays in a disclosure. */
export default function StageInspector({ canEdit, onInspectEdge, onConnect }: { canEdit: boolean; onInspectEdge: (id: string) => void; onConnect: (source: string, target: string) => void }) {
  const selectedNodeId = usePipelineStore((s) => s.selectedNodeId);
  const stage = usePipelineStore((s) => s.pipeline?.stages.find((st) => st.id === s.selectedNodeId) ?? null);
  const updateStageConfig = usePipelineStore((s) => s.updateStageConfig);
  const updateStage = usePipelineStore((s) => s.updateStage);
  const removeStage = usePipelineStore((s) => s.removeStage);
  const setSelectedNode = usePipelineStore((s) => s.setSelectedNode);
  const pipeline = usePipelineStore((s) => s.pipeline)!;
  const titleRef = useRef<HTMLHeadingElement>(null);
  const [prerequisite, setPrerequisite] = useState('');

  useEffect(() => {
    titleRef.current?.focus({ preventScroll: true });
    setPrerequisite('');
  }, [selectedNodeId]);

  if (!stage) return null;
  const set = (c: Partial<StageConfig>) => { if (canEdit) updateStageConfig(stage.id, c); };
  const incoming = pipeline.edges.filter((edge) => edge.target === stage.id);
  const descriptions: Record<Stage['type'], string> = {
    build: 'Build a container image from the configured source and Dockerfile.',
    test: 'Run the configured checks and record whether they succeed.',
    push: 'Publish the built image to the configured container registry.',
    deploy: 'Send the configured workload to its deployment target.',
    approval: 'Pause this run for the required human approval before continuing.',
    custom: 'Run your configured command or script as a pipeline step.',
  };

  return (
    <aside className="pipeline-inspector" aria-label={`Stage ${stage.name}`}>
      <div className="pipeline-inspector-heading">
        <StageSymbol kind={stage.type} />
        <Badge variant="muted">{stage.type}</Badge>
        <button type="button" onClick={() => setSelectedNode(null)} aria-label="Close inspector">
          ×
        </button>
      </div>
      <h2 ref={titleRef} tabIndex={-1}>{stage.name}</h2>
      <p>{descriptions[stage.type]}</p>
      <h3>{requirementSummary(incoming.length)}</h3>
      <p>{incoming.length > 1 ? 'Every requirement below must be met before this step is eligible to run.' : incoming.length ? 'Select the requirement to see its condition.' : 'This step has no incoming dependencies. Other steps without prerequisites may run alongside it.'}</p>
      <ol className="pipeline-prerequisites">{incoming.map((edge) => <li key={edge.id}><button type="button" onClick={() => onInspectEdge(edge.id)}><span className="pipeline-number">{pipeline.edges.indexOf(edge)+1}</span>{dependencyStory(edge, pipeline.stages).summary}</button></li>)}</ol>
      <details className="pipeline-edit-settings"><summary>{canEdit ? 'Edit step and connections' : 'Step configuration'}</summary>
      <fieldset disabled={!canEdit}>
      <Field label="Stage name"><input
        className="input"
        value={stage.name}
        onChange={(e) => { if (canEdit) updateStage(stage.id, { name: e.target.value }); }}
        aria-label="Stage name"
        spellCheck={false}
      /></Field>
      <TypeFields stage={stage} set={set} />
      <Field label="Timeout"><Text value={stage.config.timeout ?? ''} onChange={(v) => set({ timeout: v })} placeholder="30m" /></Field>
      </fieldset>
      <StageEnvironmentBindings key={stage.id} stage={stage} disabled={!canEdit} onEnvironment={(environmentId) => { if (canEdit) updateStage(stage.id, { environmentId }); }} onConfig={set} />
      {stage.type !== 'approval' && <RegistrySelection subject={{ kind: 'pipeline', id: pipeline.id }} value={stage.config.registryConnectionIds ?? []} onChange={(registryConnectionIds) => set({ registryConnectionIds })} disabled={!canEdit} label="Registry access for this step" />}
      {canEdit && <><label>Add a prerequisite<select value={prerequisite} onChange={(event) => setPrerequisite(event.target.value)}><option value="">Choose the step to wait for…</option>{pipeline.stages.filter((item) => item.id !== stage.id && !incoming.some((edge) => edge.source === item.id)).map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><button type="button" className="hud-btn" disabled={!prerequisite} onClick={() => { onConnect(prerequisite, stage.id); setPrerequisite(''); }}>Add dependency</button></>}
      <p className="mono">Stage ID: {stage.id}</p>
      {canEdit && <div className="pipeline-inspector-foot">
        <button type="button" className="pipeline-remove" onClick={() => removeStage(stage.id)}>
          Remove stage
        </button>
      </div>}
      </details>
    </aside>
  );
}
