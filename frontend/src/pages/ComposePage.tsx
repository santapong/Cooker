import { useEffect } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import ComposeCanvas from '../components/compose/ComposeCanvas';
import ServiceConfigPanel from '../components/compose/panels/ServiceConfigPanel';
import { useComposeStore } from '../stores/composeStore';
import { useTheme } from '../theme/ThemeProvider';
import { Btn, EmptyState, Pill } from '../components/ui/atoms';

export default function ComposePage() {
  const t = useTheme();
  const fetchComposeGraph = useComposeStore((s) => s.fetchComposeGraph);
  const selectedServiceName = useComposeStore((s) => s.selectedServiceName);
  const loading = useComposeStore((s) => s.loading);
  const error = useComposeStore((s) => s.error);
  const graph = useComposeStore((s) => s.graph);

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

  // Empty-state two-CTA — W11 §Indie step 2 (PR #66).
  if (!graph || graph.services.length === 0) {
    return (
      <div style={{ padding: '40px 28px' }}>
        <EmptyState
          title="No Compose file loaded."
          body="Point Cooker at a docker-compose.yml to visualise the service graph and manage services here."
          action={
            <div style={{ display: 'flex', gap: 10, justifyContent: 'center', flexWrap: 'wrap' }}>
              <Btn kind="primary" icon="layers" onClick={() => fetchComposeGraph()}>
                Open a Compose file
              </Btn>
              <a
                href="https://github.com/santapong/Cooker/blob/main/docs/user-guide/concepts/pipelines.md"
                target="_blank"
                rel="noopener noreferrer"
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 6,
                  padding: '8px 16px',
                  border: '1px solid currentColor',
                  borderRadius: 7,
                  fontSize: 13.5,
                  color: 'inherit',
                  textDecoration: 'none',
                  opacity: 0.7,
                }}
              >
                Pipelines &amp; Compose guide ↗
              </a>
            </div>
          }
        />
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
