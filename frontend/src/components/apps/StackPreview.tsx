import { useEffect, useMemo, useRef, useState } from 'react';
import { ReactFlow, ReactFlowProvider, useNodesInitialized, useReactFlow, type Edge, type Node, type NodeTypes, type EdgeTypes } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import type { RepositoryInspection } from '../../types/app';
import StarNode from '../porthole/StarNode';
import ConstellationEdge from '../porthole/ConstellationEdge';
import StageSymbol from '../pipeline/StageSymbol';
import '../porthole/porthole.css';

const nodeTypes: NodeTypes = { stage: StarNode };
const edgeTypes: EdgeTypes = { constellation: ConstellationEdge };

function PreviewMap({ preview, onSelect }: { preview: RepositoryInspection; onSelect: (name: string) => void }) {
  const box = useRef<HTMLDivElement>(null);
  const { fitView } = useReactFlow();
  const initialized = useNodesInitialized();
  useEffect(() => {
    if (!initialized || !box.current) return;
    const observer = new ResizeObserver(() => { void fitView({ padding: 0.25, maxZoom: 1 }); });
    observer.observe(box.current);
    return () => observer.disconnect();
  }, [initialized, fitView]);
  const { nodes, edges } = useMemo(() => {
    const nodes: Node[] = [];
    const edges: Edge[] = [];
    const services = preview.graph?.services ?? [];
    for (const [i, service] of services.entries()) {
      const types = service.external ? ['external'] : service.build ? ['build', 'push', 'deploy'] : ['deploy'];
      for (const [j, kind] of types.entries()) {
        const id = `${service.name}:${kind}`;
        nodes.push({ id, type: 'stage', position: { x: service.external ? 680 : kind === 'deploy' ? 450 : j * 225, y: i * 150 }, data: {
          label: service.name, stageType: kind === 'external' ? 'custom' : kind, kind,
          config: {}, status: 'idle', sub: kind === 'deploy' ? preview.workloads[service.name] : kind === 'external' ? 'Existing resource' : kind === 'build' ? service.build?.dockerfile : 'Image registry',
          service: service.name, drawDelay: 0,
        } });
        if (j > 0) edges.push({ id: `${id}:chain`, source: `${service.name}:${types[j - 1]}`, target: id, type: 'constellation', data: { state: 'idle' } });
      }
    }
    for (const [i, connection] of (preview.graph?.connections ?? []).entries()) {
      const target = services.find((s) => s.name === connection.target);
      const source = services.find((s) => s.name === connection.source);
      if (!target || !source || source.external) continue;
      edges.push({ id: `dependency:${i}`, source: `${target.name}:${target.external ? 'external' : 'deploy'}`, target: `${source.name}:deploy`, type: 'constellation', data: {
        state: 'idle', condition: target.external ? 'always' : undefined, label: target.external ? 'Connection' : undefined,
      } });
    }
    return { nodes, edges };
  }, [preview]);
  return <div ref={box} className="stack-preview-map" aria-label="Deployment graph">
    <ReactFlow defaultNodes={nodes} defaultEdges={edges} nodeTypes={nodeTypes} edgeTypes={edgeTypes}
      nodesDraggable={false} nodesConnectable={false} edgesFocusable={false} deleteKeyCode={null}
      onNodeClick={(_, node) => onSelect(String(node.data.service))} fitView fitViewOptions={{ padding: 0.25, maxZoom: 1 }} minZoom={0.2} maxZoom={1.5} proOptions={{ hideAttribution: true }} />
    <button type="button" className="hud-btn stack-fit" onClick={() => { void fitView({ padding: 0.25, maxZoom: 1 }); }}>Fit graph</button>
  </div>;
}

export default function StackPreview({ preview }: { preview: RepositoryInspection }) {
  const [selected, setSelected] = useState<string | null>(null);
  const services = preview.graph?.services ?? [];
  const service = services.find((s) => s.name === selected) ?? services[0];
  const build = preview.buildFiles.find((b) => b.service === service?.name);
  return <div className="stack-preview">
    {services.length > 0 && <>
      <ReactFlowProvider><PreviewMap preview={preview} onSelect={setSelected} /></ReactFlowProvider>
      <div className="stack-service-tabs" role="group" aria-label="Inspect a service">
        {services.map((s) => <button key={s.name} type="button" className="hud-btn" aria-pressed={service?.name === s.name} onClick={() => setSelected(s.name)}>
          <StageSymbol kind={s.external ? 'external' : 'service'} />{s.name}
        </button>)}
      </div>
      {service && <div className="stack-service-detail">
        <h3>{service.name}</h3>
        <dl className="stack-facts">
          <div><dt>Source</dt><dd>{service.external ? 'Existing database connection' : service.image || 'Build from Dockerfile'}</dd></div>
          <div><dt>Ports</dt><dd>{service.ports?.join(', ') || 'None published'}</dd></div>
          <div><dt>Environment keys</dt><dd>{Object.keys(service.environment ?? {}).join(', ') || 'None'}</dd></div>
          <div><dt>Dependencies</dt><dd>{service.dependsOn?.join(', ') || 'None'}</dd></div>
          <div><dt>Networks</dt><dd>{service.networks?.join(', ') || 'Target network'}</dd></div>
          <div><dt>Volumes</dt><dd>{service.external ? 'Managed by the existing database' : service.volumes?.join(', ') || 'None'}</dd></div>
          {build && <div><dt>Build context</dt><dd>{build.context}</dd></div>}
          {build && <div><dt>Build target</dt><dd>{service.build?.target || 'Final Dockerfile stage'}</dd></div>}
          {build && <div><dt>Build argument keys</dt><dd>{Object.keys(service.build?.args ?? {}).join(', ') || 'None'}</dd></div>}
          {service.runtimeSize && <div><dt>Runtime allocation</dt><dd>{service.runtimeSize}</dd></div>}
          <div><dt>Readiness check</dt><dd>{service.external ? 'Verify from the consuming application' : service.healthcheck ? `Every ${service.healthcheck.interval}s · ${service.healthcheck.timeout}s timeout · ${service.healthcheck.retries} retries` : 'No Compose health check configured'}</dd></div>
        </dl>
        {build && <details><summary>Dockerfile · {build.dockerfile}</summary><pre>{build.content}</pre></details>}
      </div>}
    </>}
    {preview.diagnostics.length > 0 && <div className="stack-diagnostics" role="status">
      <h3>Resolve before deployment</h3>
      <ul>{preview.diagnostics.map((d, i) => <li key={i}>{d.service && <strong>{d.service}: </strong>}{d.message}</li>)}</ul>
    </div>}
    {preview.yaml && <details className="stack-config"><summary>Resolved configuration summary · values hidden</summary><pre>{preview.yaml}</pre></details>}
  </div>;
}
