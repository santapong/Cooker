import { useEffect } from 'react';
import { useKubernetesStore } from '../stores/kubernetesStore';

export default function KubernetesPage() {
  const { namespaces, workloads, selectedNamespace, loading, fetchNamespaces, fetchWorkloads, setNamespace } = useKubernetesStore();

  useEffect(() => {
    fetchNamespaces();
    fetchWorkloads();
  }, [fetchNamespaces, fetchWorkloads]);

  return (
    <div>
      <h2 style={styles.title}>Kubernetes Cluster</h2>

      <div style={styles.controls}>
        <label style={styles.label}>Namespace:</label>
        <select
          style={styles.select}
          value={selectedNamespace}
          onChange={(e) => setNamespace(e.target.value)}
        >
          <option value="">All namespaces</option>
          {namespaces.map((ns) => (
            <option key={ns.name} value={ns.name}>{ns.name}</option>
          ))}
        </select>
      </div>

      <section style={styles.section}>
        <h3 style={styles.sectionTitle}>Workloads</h3>
        {loading ? (
          <p style={styles.muted}>Loading...</p>
        ) : workloads.length === 0 ? (
          <p style={styles.muted}>No workloads found in the selected namespace.</p>
        ) : (
          <table style={styles.table}>
            <thead>
              <tr>
                <th style={styles.th}>Name</th>
                <th style={styles.th}>Kind</th>
                <th style={styles.th}>Namespace</th>
                <th style={styles.th}>Ready</th>
                <th style={styles.th}>Image</th>
                <th style={styles.th}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {workloads.map((w) => (
                <tr key={`${w.namespace}-${w.kind}-${w.name}`}>
                  <td style={styles.td}>{w.name}</td>
                  <td style={styles.td}>{w.kind}</td>
                  <td style={styles.td}>{w.namespace}</td>
                  <td style={styles.td}>
                    <span style={{ color: w.ready === w.replicas ? '#22c55e' : '#f59e0b' }}>
                      {w.ready}/{w.replicas}
                    </span>
                  </td>
                  <td style={styles.td}>{w.image}</td>
                  <td style={styles.td}>
                    <button className="btn-secondary" style={{ padding: '2px 8px', fontSize: 11 }}>Scale</button>
                    <button className="btn-secondary" style={{ padding: '2px 8px', fontSize: 11, marginLeft: 4 }}>Restart</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  title: { fontSize: 22, fontWeight: 700, color: '#f1f5f9', margin: '0 0 20px' },
  controls: { display: 'flex', alignItems: 'center', gap: 8, marginBottom: 20 },
  label: { color: '#94a3b8', fontSize: 13 },
  select: {
    padding: '6px 10px', borderRadius: 4, border: '1px solid #475569',
    backgroundColor: '#1e293b', color: '#f1f5f9', fontSize: 13,
  },
  section: { marginBottom: 32 },
  sectionTitle: { fontSize: 16, fontWeight: 600, color: '#f1f5f9', margin: '0 0 12px' },
  muted: { color: '#94a3b8', fontSize: 14 },
  table: { width: '100%', borderCollapse: 'collapse' as const },
  th: { textAlign: 'left' as const, padding: '8px 12px', fontSize: 12, color: '#94a3b8', borderBottom: '1px solid #334155' },
  td: { padding: '8px 12px', fontSize: 13, color: '#f1f5f9', borderBottom: '1px solid #1e293b' },
};
