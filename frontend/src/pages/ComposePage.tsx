import { useEffect, useState, type FormEvent } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import { useComposeStore } from '../stores/composeStore';
import { useComposeLibraryStore } from '../stores/composeLibraryStore';
import Porthole from '../components/porthole/Porthole';
import { SceneContext } from '../components/porthole/sceneContext';
import ComposeCanvas from '../components/instruments/ComposeCanvas';
import ComposeInspector from '../components/instruments/ComposeInspector';
import ComposeLibrary from '../components/instruments/ComposeLibrary';
import { FormError, TextInput } from '../components/ui/form';
import { pushToast } from '../stores/toastStore';
import type { ComposeServicePatch } from '../types/compose';
import '../components/instruments/compose.css';

/** A saved file library alongside the live, editable Compose map. */
export default function ComposePage() {
  const { graph, composePath, loading, error, fetchComposeGraph, selectedServiceName: selectedName, setSelectedService: setSelected, updateServiceConfig } = useComposeStore();
  const stackName = useComposeLibraryStore((s) => s.stacks.find((stack) => stack.composePath === composePath)?.name);
  const [path, setPath] = useState(composePath);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!graph && !loading && !error) void fetchComposeGraph();
    // load once on first visit
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const open = async (filename: string) => {
    const next = filename.trim() || 'docker-compose.yml';
    setPath(next);
    await fetchComposeGraph(next);
  };
  const parse = (e: FormEvent) => {
    e.preventDefault();
    void open(path);
  };
  const svc = graph?.services.find((s) => s.name === selectedName) ?? null;
  const save = async (patch: ComposeServicePatch) => {
    if (!svc) return;
    setSaving(true);
    try {
      const res = await updateServiceConfig(svc.name, patch);
      pushToast('success', svc.name + ': ' + (res.message || 'service config updated') + '.');
    } finally {
      setSaving(false);
    }
  };
  const counts = graph ? graph.services.length + ' services · ' + graph.connections.length + ' links · ' + graph.networks.length + ' networks · ' + graph.volumes.length + ' volumes' : '';

  return (
    <div className="editor compose-workspace">
      <ComposeLibrary graph={graph} composePath={composePath} busy={loading || saving} onOpen={(filename) => { void open(filename); }} />
      <div className="compose-main">
        <div className="compose-file-bar">
          <div><span className="caps">Compose map</span><p className="compose-muted" id="compose-file-hint">Filename in the build host's Compose directory.</p></div>
          <form className="compose-file-form" onSubmit={parse}>
            <TextInput value={path} onChange={(e) => setPath(e.target.value)} placeholder="docker-compose.yml" aria-label="Compose file path" aria-describedby="compose-file-hint" disabled={loading || saving} />
            <button type="submit" className="hud-btn hud-btn-primary" disabled={loading || saving}>{loading ? 'Parsing…' : 'Parse'}</button>
          </form>
        </div>
        <FormError>{error && <>{error}{graph && <span> · Still showing {composePath}.</span>}</>}</FormError>
        <Porthole
          className="compose-porthole"
          starfieldSeed={21}
          title={<h1 tabIndex={-1} className="caps hud-title">{stackName || 'Porthole · Compose'}{graph && <span className="compose-map-file mono">{composePath}</span>}</h1>}
        >
          {graph && graph.services.length > 0 && (
            <SceneContext.Provider value={{ now: 0, selectedId: selectedName }}>
              <div className="run-canvas console-closed" style={{ top: 72, bottom: 76 }}>
                <ReactFlowProvider>
                  <ComposeCanvas key={composePath} graph={graph} onSelect={loading || saving ? () => {} : setSelected} />
                </ReactFlowProvider>
              </div>
            </SceneContext.Provider>
          )}
          {(!graph || graph.services.length === 0) && <div className="porthole-empty"><p>{loading ? 'Parsing…' : graph ? 'This Compose file has no services.' : 'Open a Compose file to explore its services and connections.'}</p></div>}
          {graph && <div className="hud hud-bl compose-map-footer"><span className="mono hud-stats">{counts}</span><span className="compose-muted">Select a service to inspect and edit</span></div>}
          {svc && <ComposeInspector key={composePath + ':' + svc.name} service={svc} busy={saving || loading} onSave={save} onClose={() => setSelected(null)} />}
        </Porthole>
      </div>
    </div>
  );
}
