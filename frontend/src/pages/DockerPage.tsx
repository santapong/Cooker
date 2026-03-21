import { useEffect } from 'react';
import { useDockerStore } from '../stores/dockerStore';

export default function DockerPage() {
  const { images, containers, loading, fetchImages, fetchContainers } = useDockerStore();

  useEffect(() => {
    fetchImages();
    fetchContainers();
  }, [fetchImages, fetchContainers]);

  return (
    <div>
      <h2 style={styles.title}>Docker Management</h2>

      <section style={styles.section}>
        <div style={styles.sectionHeader}>
          <h3 style={styles.sectionTitle}>Images</h3>
          <button className="btn-primary">Build Image</button>
        </div>
        {loading ? (
          <p style={styles.muted}>Loading...</p>
        ) : images.length === 0 ? (
          <p style={styles.muted}>No images found. Build or pull an image to get started.</p>
        ) : (
          <table style={styles.table}>
            <thead>
              <tr>
                <th style={styles.th}>Repository</th>
                <th style={styles.th}>Tag</th>
                <th style={styles.th}>Size</th>
                <th style={styles.th}>Created</th>
              </tr>
            </thead>
            <tbody>
              {images.map((img) => (
                <tr key={img.id}>
                  <td style={styles.td}>{img.repoTags?.[0] || img.id.slice(0, 12)}</td>
                  <td style={styles.td}>{img.repoTags?.[0]?.split(':')[1] || 'latest'}</td>
                  <td style={styles.td}>{(img.size / 1024 / 1024).toFixed(1)} MB</td>
                  <td style={styles.td}>{new Date(img.created).toLocaleDateString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section style={styles.section}>
        <div style={styles.sectionHeader}>
          <h3 style={styles.sectionTitle}>Containers</h3>
          <button className="btn-primary">Create Container</button>
        </div>
        {containers.length === 0 ? (
          <p style={styles.muted}>No containers running.</p>
        ) : (
          <table style={styles.table}>
            <thead>
              <tr>
                <th style={styles.th}>Name</th>
                <th style={styles.th}>Image</th>
                <th style={styles.th}>State</th>
                <th style={styles.th}>Status</th>
              </tr>
            </thead>
            <tbody>
              {containers.map((c) => (
                <tr key={c.id}>
                  <td style={styles.td}>{c.name}</td>
                  <td style={styles.td}>{c.image}</td>
                  <td style={styles.td}>
                    <span style={{ color: c.state === 'running' ? '#22c55e' : '#ef4444' }}>{c.state}</span>
                  </td>
                  <td style={styles.td}>{c.status}</td>
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
  section: { marginBottom: 32 },
  sectionHeader: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 },
  sectionTitle: { fontSize: 16, fontWeight: 600, color: '#f1f5f9', margin: 0 },
  muted: { color: '#94a3b8', fontSize: 14 },
  table: { width: '100%', borderCollapse: 'collapse' as const },
  th: { textAlign: 'left' as const, padding: '8px 12px', fontSize: 12, color: '#94a3b8', borderBottom: '1px solid #334155' },
  td: { padding: '8px 12px', fontSize: 13, color: '#f1f5f9', borderBottom: '1px solid #1e293b' },
};
