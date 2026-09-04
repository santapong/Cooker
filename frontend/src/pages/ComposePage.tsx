import { useEffect, useState, type FormEvent } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import { useComposeStore } from '../stores/composeStore';
import Porthole from '../components/porthole/Porthole';
import { SceneContext } from '../components/porthole/sceneContext';
import ComposeCanvas from '../components/instruments/ComposeCanvas';
import Badge from '../components/ui/Badge';
import Caps from '../components/ui/Caps';
import { Actions, TextInput } from '../components/ui/form';

/** Compose — parse a compose file on the build host and see its services as a constellation. */
export default function ComposePage() {
  const graph = useComposeStore((s) => s.graph);
  const loading = useComposeStore((s) => s.loading);
  const error = useComposeStore((s) => s.error);
  const fetchComposeGraph = useComposeStore((s) => s.fetchComposeGraph);
  const selectedName = useComposeStore((s) => s.selectedServiceName);
  const setSelected = useComposeStore((s) => s.setSelectedService);
  const [path, setPath] = useState('docker-compose.yml');

  useEffect(() => {
    if (!graph && !loading && !error) void fetchComposeGraph();
    // load once on first visit
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const parse = (e: FormEvent) => {
    e.preventDefault();
    void fetchComposeGraph(path.trim() || undefined);
  };
  const svc = graph?.services.find((s) => s.name === selectedName) ?? null;
  const counts = graph ? `${graph.services.length} services · ${graph.connections.length} links · ${graph.networks.length} networks · ${graph.volumes.length} volumes` : '';

  return (
    <div className="editor">
      <Porthole
        starfieldSeed={21}
        title={
          <h1 tabIndex={-1} className="caps hud-title">
            Porthole · Compose{graph ? ` · ${path}` : ''}
          </h1>
        }
        hudRight={
          <form className="hud-actions" onSubmit={parse}>
            <TextInput value={path} onChange={(e) => setPath(e.target.value)} placeholder="docker-compose.yml" aria-label="Compose file path" style={{ width: 220, height: 26 }} />
            <button type="submit" className="hud-btn hud-btn-primary" disabled={loading}>
              {loading ? 'Parsing…' : 'Parse'}
            </button>
          </form>
        }
      >
        {graph && graph.services.length > 0 && (
          <SceneContext.Provider value={{ now: 0, selectedId: selectedName }}>
            <div className="run-canvas console-closed" style={{ bottom: 0 }}>
              <ReactFlowProvider>
                <ComposeCanvas graph={graph} onSelect={setSelected} />
              </ReactFlowProvider>
            </div>
          </SceneContext.Provider>
        )}
        {(!graph || graph.services.length === 0) && (
          <div className="porthole-empty">
            <div>
              <p>{error ? error : loading ? 'Parsing…' : 'No compose file loaded. Enter a path on the build host and parse it.'}</p>
            </div>
          </div>
        )}
        {graph && (
          <div className="hud hud-bl">
            <span className="mono hud-stats">{counts}</span>
          </div>
        )}
        {svc && (
          <aside className="inspector" aria-label={`Service ${svc.name}`}>
            <div className="inspector-head">
              <Badge variant="muted">service</Badge>
              {svc.status && <Badge variant={svc.status === 'running' ? 'ok' : 'muted'}>{svc.status}</Badge>}
              <span className="spacer" />
              <button type="button" className="inspector-close" onClick={() => setSelected(null)} aria-label="Close inspector">
                ×
              </button>
            </div>
            <h2>{svc.name}</h2>
            <div className="kv">
              <Caps>Image</Caps>
              <span className={svc.image ? 'v' : 'v muted'}>{svc.image || (svc.build ? `build ${svc.build.context}${svc.build.dockerfile ? ` (${svc.build.dockerfile})` : ''}` : '—')}</span>
              <Caps>Ports</Caps>
              <span className={svc.ports?.length ? 'v' : 'v muted'}>{svc.ports?.join(', ') || '—'}</span>
              <Caps>Depends on</Caps>
              <span className={svc.dependsOn?.length ? 'v' : 'v muted'}>{svc.dependsOn?.join(', ') || '—'}</span>
              <Caps>Networks</Caps>
              <span className={svc.networks?.length ? 'v' : 'v muted'}>{svc.networks?.join(', ') || '—'}</span>
              <Caps>Volumes</Caps>
              <span className={svc.volumes?.length ? 'v' : 'v muted'}>{svc.volumes?.join(', ') || '—'}</span>
              <Caps>Command</Caps>
              <span className={svc.command ? 'v' : 'v muted'}>{svc.command || '—'}</span>
              <Caps>Env</Caps>
              <span className={Object.keys(svc.environment ?? {}).length ? 'v' : 'v muted'}>{Object.keys(svc.environment ?? {}).join(', ') || '—'}</span>
            </div>
            <Actions>
              <span className="mono" style={{ fontSize: 12, color: 'var(--ink-3)' }}>
                edits land in P5c
              </span>
            </Actions>
          </aside>
        )}
      </Porthole>
    </div>
  );
}
