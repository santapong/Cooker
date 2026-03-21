import { useEffect, useState } from 'react';
import { useEnvironmentStore } from '../stores/environmentStore';

export default function EnvironmentsPage() {
  const { environments, loading, fetchEnvironments, createEnvironment } = useEnvironmentStore();
  const [showCreate, setShowCreate] = useState(false);

  useEffect(() => {
    fetchEnvironments();
  }, [fetchEnvironments]);

  const handleCreate = async (name: string, order: number, strategy: 'auto' | 'manual') => {
    await createEnvironment({
      name,
      order,
      target: { type: 'namespace', clusterId: '', namespace: `cooker-${name}`, kubeContext: '' },
      promotion: { strategy },
      variables: {},
    });
    setShowCreate(false);
  };

  const envColors: Record<string, string> = {
    dev: '#3b82f6',
    staging: '#f59e0b',
    production: '#22c55e',
  };

  return (
    <div>
      <div style={styles.header}>
        <h2 style={styles.title}>Environments</h2>
        <button className="btn-primary" onClick={() => setShowCreate(true)}>Add Environment</button>
      </div>

      <div style={styles.envFlow}>
        {environments.length === 0 && !loading ? (
          <div style={styles.empty}>
            <p>No environments configured.</p>
            <p style={styles.muted}>Set up Dev, Staging, and Production environments for your deployment pipeline.</p>
            <div style={{ display: 'flex', gap: 8, marginTop: 12, justifyContent: 'center' }}>
              <button className="btn-primary" onClick={() => handleCreate('dev', 1, 'auto')}>Add Dev</button>
              <button className="btn-primary" onClick={() => handleCreate('staging', 2, 'auto')}>Add Staging</button>
              <button className="btn-primary" onClick={() => handleCreate('production', 3, 'manual')}>Add Production</button>
            </div>
          </div>
        ) : (
          environments
            .sort((a, b) => a.order - b.order)
            .map((env, i) => (
              <div key={env.id} style={{ display: 'flex', alignItems: 'center' }}>
                <div style={{ ...styles.envCard, borderColor: envColors[env.name] || '#475569' }}>
                  <div style={styles.envName}>{env.name.toUpperCase()}</div>
                  <div style={styles.envMeta}>
                    Target: {env.target.type === 'namespace' ? `ns/${env.target.namespace}` : `cluster/${env.target.clusterId}`}
                  </div>
                  <div style={styles.envMeta}>
                    Promotion: <span style={{ color: env.promotion.strategy === 'auto' ? '#22c55e' : '#f59e0b' }}>
                      {env.promotion.strategy}
                    </span>
                  </div>
                  <div style={styles.envVars}>
                    {Object.keys(env.variables).length} variables
                  </div>
                </div>
                {i < environments.length - 1 && (
                  <div style={styles.arrow}>-&gt;</div>
                )}
              </div>
            ))
        )}
      </div>

      {showCreate && (
        <div style={styles.modal}>
          <p style={{ color: '#94a3b8' }}>Use the quick setup buttons above or configure via API.</p>
          <button className="btn-secondary" onClick={() => setShowCreate(false)}>Close</button>
        </div>
      )}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  header: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 },
  title: { fontSize: 22, fontWeight: 700, color: '#f1f5f9', margin: 0 },
  envFlow: { display: 'flex', alignItems: 'center', gap: 0, justifyContent: 'center', flexWrap: 'wrap' },
  envCard: {
    padding: 20,
    backgroundColor: '#1e293b',
    borderRadius: 8,
    border: '2px solid #475569',
    minWidth: 200,
  },
  envName: { fontSize: 16, fontWeight: 700, color: '#f1f5f9', marginBottom: 8 },
  envMeta: { fontSize: 12, color: '#94a3b8', marginBottom: 4 },
  envVars: { fontSize: 11, color: '#475569', marginTop: 8 },
  arrow: { fontSize: 24, color: '#475569', padding: '0 12px' },
  empty: { textAlign: 'center', padding: 40, color: '#f1f5f9' },
  muted: { color: '#94a3b8', fontSize: 14 },
  modal: { marginTop: 20, padding: 20, backgroundColor: '#1e293b', borderRadius: 8, border: '1px solid #334155' },
};
