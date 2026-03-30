import { useComposeStore } from '../../../stores/composeStore';

export default function ServiceConfigPanel() {
  const { graph, selectedServiceName, setSelectedService, updateServiceConfig } = useComposeStore();

  if (!graph || !selectedServiceName) return null;

  const service = graph.services.find((s) => s.name === selectedServiceName);
  if (!service) return null;

  const envEntries = Object.entries(service.environment || {});

  return (
    <div style={styles.panel}>
      <div style={styles.header}>
        <h3 style={styles.title}>{service.name}</h3>
        <button style={styles.closeBtn} onClick={() => setSelectedService(null)}>x</button>
      </div>

      <div style={styles.section}>
        <label style={styles.label}>Image</label>
        <input
          style={styles.input}
          value={service.image || ''}
          onChange={(e) =>
            updateServiceConfig(service.name, { image: e.target.value })
          }
          placeholder="image:tag"
        />
      </div>

      {service.build && (
        <div style={styles.section}>
          <label style={styles.label}>Build</label>
          <div style={styles.badge}>{service.build.context}</div>
          {service.build.dockerfile && (
            <div style={{ ...styles.detail, marginTop: 4 }}>
              Dockerfile: {service.build.dockerfile}
            </div>
          )}
        </div>
      )}

      {service.ports.length > 0 && (
        <div style={styles.section}>
          <label style={styles.label}>Ports</label>
          {service.ports.map((p, i) => (
            <div key={i} style={styles.badge}>{p}</div>
          ))}
        </div>
      )}

      {service.dependsOn.length > 0 && (
        <div style={styles.section}>
          <label style={styles.label}>Depends On</label>
          {service.dependsOn.map((d) => (
            <div
              key={d}
              style={{ ...styles.badge, cursor: 'pointer' }}
              onClick={() => setSelectedService(d)}
            >
              {d}
            </div>
          ))}
        </div>
      )}

      {envEntries.length > 0 && (
        <div style={styles.section}>
          <label style={styles.label}>Environment</label>
          {envEntries.map(([key, val]) => (
            <div key={key} style={styles.envRow}>
              <span style={styles.envKey}>{key}</span>
              <span style={styles.envVal}>{val}</span>
            </div>
          ))}
        </div>
      )}

      {service.volumes.length > 0 && (
        <div style={styles.section}>
          <label style={styles.label}>Volumes</label>
          {service.volumes.map((v, i) => (
            <div key={i} style={styles.detail}>{v}</div>
          ))}
        </div>
      )}

      {service.command && (
        <div style={styles.section}>
          <label style={styles.label}>Command</label>
          <div style={styles.detail}>{service.command}</div>
        </div>
      )}

      {service.networks.length > 0 && (
        <div style={styles.section}>
          <label style={styles.label}>Networks</label>
          {service.networks.map((n) => (
            <div key={n} style={styles.badge}>{n}</div>
          ))}
        </div>
      )}
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  panel: {
    width: 280,
    backgroundColor: '#1e293b',
    borderLeft: '1px solid #334155',
    padding: 16,
    overflowY: 'auto',
  },
  header: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 },
  title: { fontSize: 14, fontWeight: 600, color: '#f1f5f9', margin: 0 },
  closeBtn: { background: 'none', border: 'none', color: '#94a3b8', fontSize: 16, cursor: 'pointer', padding: 4 },
  section: { marginBottom: 12 },
  label: { display: 'block', fontSize: 11, color: '#94a3b8', marginBottom: 4, textTransform: 'uppercase' as const },
  input: {
    width: '100%',
    padding: '6px 10px',
    borderRadius: 4,
    border: '1px solid #475569',
    backgroundColor: '#0f172a',
    color: '#f1f5f9',
    fontSize: 13,
    outline: 'none',
  },
  badge: {
    display: 'inline-block',
    padding: '2px 8px',
    borderRadius: 4,
    backgroundColor: '#334155',
    color: '#0ea5e9',
    fontSize: 12,
    fontWeight: 600,
    marginRight: 4,
    marginBottom: 4,
  },
  detail: { fontSize: 11, color: '#94a3b8' },
  envRow: {
    display: 'flex',
    flexDirection: 'column' as const,
    padding: '4px 0',
    borderBottom: '1px solid #0f172a',
  },
  envKey: { fontSize: 11, color: '#0ea5e9', fontWeight: 600 },
  envVal: { fontSize: 11, color: '#94a3b8', wordBreak: 'break-all' as const },
};
