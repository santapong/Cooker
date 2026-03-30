import { useEffect } from 'react';
import { ReactFlowProvider } from '@xyflow/react';
import ComposeCanvas from '../components/compose/ComposeCanvas';
import ServiceConfigPanel from '../components/compose/panels/ServiceConfigPanel';
import { useComposeStore } from '../stores/composeStore';

export default function ComposePage() {
  const { fetchComposeGraph, selectedServiceName, loading, error } = useComposeStore();

  useEffect(() => {
    fetchComposeGraph();
  }, [fetchComposeGraph]);

  if (loading) {
    return <div style={{ color: '#94a3b8', padding: 40, textAlign: 'center' }}>Loading compose graph...</div>;
  }

  if (error) {
    return <div style={{ color: '#ef4444', padding: 40, textAlign: 'center' }}>Error: {error}</div>;
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: 'calc(100vh - 72px)', margin: -20 }}>
      <div style={styles.toolbar}>
        <h2 style={styles.title}>Docker Compose</h2>
        <button className="btn-primary" onClick={() => fetchComposeGraph()}>
          Refresh
        </button>
      </div>
      <div style={{ display: 'flex', flex: 1 }}>
        <ReactFlowProvider>
          <ComposeCanvas />
        </ReactFlowProvider>
        {selectedServiceName && <ServiceConfigPanel />}
      </div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  toolbar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '8px 16px',
    backgroundColor: '#1e293b',
    borderBottom: '1px solid #334155',
  },
  title: { fontSize: 16, fontWeight: 600, color: '#f1f5f9', margin: 0 },
};
