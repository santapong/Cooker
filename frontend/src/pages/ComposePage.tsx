import { useEffect } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import ComposeCanvas from '../components/compose/ComposeCanvas';
import ServiceConfigPanel from '../components/compose/panels/ServiceConfigPanel';
import { useComposeStore } from '../stores/composeStore';
import { useTheme } from '../theme/ThemeProvider';
import { Btn, Pill } from '../components/ui/atoms';

export default function ComposePage() {
  const t = useTheme();
  const { fetchComposeGraph, selectedServiceName, loading, error, graph } = useComposeStore();

  useEffect(() => {
    fetchComposeGraph();
  }, [fetchComposeGraph]);

  if (loading) {
    return (
      <div
        style={{
          height: '100%',
          display: 'grid',
          placeItems: 'center',
          color: t.textMute,
          fontFamily: t.serif,
          fontSize: 18,
        }}
      >
        Loading compose graph…
      </div>
    );
  }

  if (error) {
    return (
      <div
        style={{
          height: '100%',
          display: 'grid',
          placeItems: 'center',
          color: t.bad,
          fontFamily: t.mono,
          fontSize: 13,
        }}
      >
        {error}
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <div
        style={{
          padding: '12px 18px',
          borderBottom: `1px solid ${t.line}`,
          background: t.surface,
          display: 'flex',
          alignItems: 'center',
          gap: 12,
        }}
      >
        <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
          docker-compose graph
        </span>
        <Pill>{graph?.services.length ?? 0} services</Pill>
        <div style={{ flex: 1 }} />
        <Btn kind="secondary" icon="cog" onClick={() => fetchComposeGraph()}>
          Refresh
        </Btn>
      </div>
      <div style={{ display: 'flex', flex: 1, minHeight: 0 }}>
        <ReactFlowProvider>
          <ComposeCanvas />
        </ReactFlowProvider>
        {selectedServiceName && <ServiceConfigPanel />}
      </div>
    </div>
  );
}
