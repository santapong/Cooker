import { useEffect, useRef, type ReactNode } from 'react';
import { usePipelineStore } from '../../stores/pipelineStore';
import type { Stage, StageConfig } from '../../types/pipeline';
import Badge from '../ui/Badge';
import Caps from '../ui/Caps';

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
          <Field label="Namespace"><Text value={c.namespace ?? ''} onChange={(v) => set({ namespace: v })} placeholder="default" /></Field>
          <Field label="Manifest path"><Text value={c.manifestPath ?? ''} onChange={(v) => set({ manifestPath: v })} placeholder="k8s/deployment.yaml" /></Field>
          <Field label="Helm chart"><Text value={c.helmChart ?? ''} onChange={(v) => set({ helmChart: v })} placeholder="oci://ghcr.io/org/chart" /></Field>
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

/** Right-hand stage inspector — slides in when a star is selected. */
export default function StageInspector() {
  const selectedNodeId = usePipelineStore((s) => s.selectedNodeId);
  const stage = usePipelineStore((s) => s.pipeline?.stages.find((st) => st.id === s.selectedNodeId) ?? null);
  const updateStageConfig = usePipelineStore((s) => s.updateStageConfig);
  const updateStage = usePipelineStore((s) => s.updateStage);
  const removeStage = usePipelineStore((s) => s.removeStage);
  const setSelectedNode = usePipelineStore((s) => s.setSelectedNode);
  const nameRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    nameRef.current?.focus({ preventScroll: true });
  }, [selectedNodeId]);

  if (!stage) return null;
  const set = (c: Partial<StageConfig>) => updateStageConfig(stage.id, c);

  return (
    <aside className="inspector" aria-label={`Stage ${stage.name}`}>
      <div className="inspector-head">
        <Badge variant="muted">{stage.type}</Badge>
        <span className="spacer" />
        <button type="button" className="inspector-close" onClick={() => setSelectedNode(null)} aria-label="Close inspector">
          ×
        </button>
      </div>
      <input
        ref={nameRef}
        className="inspector-name"
        value={stage.name}
        onChange={(e) => updateStage(stage.id, { name: e.target.value })}
        aria-label="Stage name"
        spellCheck={false}
      />
      <TypeFields stage={stage} set={set} />
      <Field label="Timeout"><Text value={stage.config.timeout ?? ''} onChange={(v) => set({ timeout: v })} placeholder="30m" /></Field>
      <div className="inspector-foot">
        <button type="button" className="btn-danger" onClick={() => removeStage(stage.id)}>
          Remove stage
        </button>
      </div>
    </aside>
  );
}
