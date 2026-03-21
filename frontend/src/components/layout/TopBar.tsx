export default function TopBar() {
  return (
    <header style={styles.header}>
      <div style={styles.left}>
        <h1 style={styles.title}>Dashboard</h1>
      </div>
      <div style={styles.right}>
        <span style={styles.envBadge}>Dev</span>
        <span style={{ ...styles.envBadge, backgroundColor: '#f59e0b' }}>Staging</span>
        <span style={{ ...styles.envBadge, backgroundColor: '#22c55e' }}>Production</span>
        <div style={styles.user}>Dev User</div>
      </div>
    </header>
  );
}

const styles: Record<string, React.CSSProperties> = {
  header: {
    height: 52,
    backgroundColor: '#1e293b',
    borderBottom: '1px solid #334155',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 20px',
  },
  left: { display: 'flex', alignItems: 'center', gap: 16 },
  title: { fontSize: 16, fontWeight: 600, color: '#f1f5f9', margin: 0 },
  right: { display: 'flex', alignItems: 'center', gap: 8 },
  envBadge: {
    padding: '3px 8px',
    borderRadius: 4,
    fontSize: 11,
    fontWeight: 600,
    backgroundColor: '#3b82f6',
    color: 'white',
  },
  user: {
    marginLeft: 12,
    padding: '4px 12px',
    borderRadius: 6,
    backgroundColor: '#334155',
    color: '#94a3b8',
    fontSize: 13,
  },
};
