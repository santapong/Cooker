import { useComposeStore } from '../../../stores/composeStore';
import { useTheme } from '../../../theme/ThemeProvider';
import { hexA } from '../../../theme/tokens';

export default function ServiceConfigPanel() {
  const t = useTheme();
  const { graph, selectedServiceName, setSelectedService, updateServiceConfig } = useComposeStore();

  if (!graph || !selectedServiceName) return null;

  const service = graph.services.find((s) => s.name === selectedServiceName);
  if (!service) return null;

  const envEntries = Object.entries(service.environment || {});

  const styles: Record<string, React.CSSProperties> = {
    panel: {
      width: 296,
      background: t.panelGlass,
      backdropFilter: 'blur(12px)',
      WebkitBackdropFilter: 'blur(12px)',
      borderLeft: `1px solid ${t.line}`,
      padding: 16,
      overflowY: 'auto',
      color: t.text,
    },
    header: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 },
    title: { fontFamily: t.display, fontSize: 16, fontWeight: 600, color: t.text, margin: 0, letterSpacing: -0.3 },
    closeBtn: { background: 'none', border: 'none', color: t.textMute, fontSize: 16, cursor: 'pointer', padding: 4 },
    section: { marginBottom: 12 },
    label: {
      display: 'block',
      fontFamily: t.mono,
      fontSize: 10,
      letterSpacing: 1,
      color: t.textMute,
      marginBottom: 5,
      textTransform: 'uppercase' as const,
    },
    input: {
      width: '100%',
      padding: '7px 10px',
      borderRadius: 8,
      border: `1px solid ${t.line}`,
      background: hexA(t.surfaceSolid, 0.5),
      color: t.text,
      fontSize: 13,
      fontFamily: t.sans,
      outline: 'none',
    },
    badge: {
      display: 'inline-block',
      padding: '2px 9px',
      borderRadius: 999,
      background: hexA(t.cyan, 0.12),
      color: t.cyan,
      border: `1px solid ${hexA(t.cyan, 0.3)}`,
      fontFamily: t.mono,
      fontSize: 11,
      fontWeight: 600,
      marginRight: 4,
      marginBottom: 4,
    },
    detail: { fontFamily: t.mono, fontSize: 11, color: t.textSoft },
    envRow: {
      display: 'flex',
      flexDirection: 'column' as const,
      padding: '4px 0',
      borderBottom: `1px solid ${t.line}`,
    },
    envKey: { fontFamily: t.mono, fontSize: 11, color: t.cyan, fontWeight: 600 },
    envVal: { fontFamily: t.mono, fontSize: 11, color: t.textSoft, wordBreak: 'break-all' as const },
  };

  return (
    <div style={styles.panel}>
      <div style={styles.header}>
        <h3 style={styles.title}>{service.name}</h3>
        <button style={styles.closeBtn} onClick={() => setSelectedService(null)}>
          ✕
        </button>
      </div>

      <div style={styles.section}>
        <label style={styles.label}>Image</label>
        <input
          className="cc-field"
          style={styles.input}
          value={service.image || ''}
          onChange={(e) => updateServiceConfig(service.name, { image: e.target.value })}
          placeholder="image:tag"
        />
      </div>

      {service.build && (
        <div style={styles.section}>
          <label style={styles.label}>Build</label>
          <div style={styles.badge}>{service.build.context}</div>
          {service.build.dockerfile && (
            <div style={{ ...styles.detail, marginTop: 4 }}>Dockerfile: {service.build.dockerfile}</div>
          )}
        </div>
      )}

      {service.ports.length > 0 && (
        <div style={styles.section}>
          <label style={styles.label}>Ports</label>
          {service.ports.map((p, i) => (
            <div key={i} style={styles.badge}>
              {p}
            </div>
          ))}
        </div>
      )}

      {service.dependsOn.length > 0 && (
        <div style={styles.section}>
          <label style={styles.label}>Depends On</label>
          {service.dependsOn.map((d) => (
            <div key={d} style={{ ...styles.badge, cursor: 'pointer' }} onClick={() => setSelectedService(d)}>
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
            <div key={i} style={styles.detail}>
              {v}
            </div>
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
            <div key={n} style={styles.badge}>
              {n}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
