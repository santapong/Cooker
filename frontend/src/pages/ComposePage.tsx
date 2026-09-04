import { useEffect, useState, type FormEvent } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import { useComposeStore } from '../stores/composeStore';
import Porthole from '../components/porthole/Porthole';
import { SceneContext } from '../components/porthole/sceneContext';
import ComposeCanvas from '../components/instruments/ComposeCanvas';
import ComposeInspector from '../components/instruments/ComposeInspector';
import { TextInput } from '../components/ui/form';
import { pushToast } from '../stores/toastStore';
import type { ComposeServicePatch } from '../types/compose';

/** Compose — parse a compose file on the build host and see its services as a constellation. */
export default function ComposePage() {
  const graph = useComposeStore((s) => s.graph);
  const loading = useComposeStore((s) => s.loading);
  const error = useComposeStore((s) => s.error);
  const fetchComposeGraph = useComposeStore((s) => s.fetchComposeGraph);
  const selectedName = useComposeStore((s) => s.selectedServiceName);
  const setSelected = useComposeStore((s) => s.setSelectedService);
  const updateServiceConfig = useComposeStore((s) => s.updateServiceConfig);
  const [path, setPath] = useState('docker-compose.yml');
  const [saving, setSaving] = useState(false);

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
  const save = async (patch: ComposeServicePatch) => {
    if (!svc) return;
    setSaving(true);
    try {
      const res = await updateServiceConfig(svc.name, patch);
      pushToast('success', `${svc.name}: ${res.message || 'service config updated'}.`);
    } finally {
      setSaving(false);
    }
  };
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
        {svc && <ComposeInspector key={svc.name} service={svc} busy={saving} onSave={save} onClose={() => setSelected(null)} />}
      </Porthole>
    </div>
  );
}
