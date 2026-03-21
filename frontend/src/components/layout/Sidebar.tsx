import { Link, useLocation } from 'react-router-dom';

const navItems = [
  { path: '/pipelines', label: 'Pipelines', icon: 'P' },
  { path: '/docker', label: 'Docker', icon: 'D' },
  { path: '/kubernetes', label: 'Kubernetes', icon: 'K' },
  { path: '/environments', label: 'Environments', icon: 'E' },
];

export default function Sidebar() {
  const location = useLocation();

  return (
    <aside style={styles.sidebar}>
      <div style={styles.logo}>
        <h2 style={styles.logoText}>Cooker</h2>
        <span style={styles.logoSubtext}>CI/CD Manager</span>
      </div>
      <nav style={styles.nav}>
        {navItems.map((item) => (
          <Link
            key={item.path}
            to={item.path}
            style={{
              ...styles.navItem,
              ...(location.pathname.startsWith(item.path) ? styles.navItemActive : {}),
            }}
          >
            <span style={styles.navIcon}>{item.icon}</span>
            {item.label}
          </Link>
        ))}
      </nav>
      <div style={styles.footer}>
        <span style={styles.footerText}>OCI Compliant</span>
      </div>
    </aside>
  );
}

const styles: Record<string, React.CSSProperties> = {
  sidebar: {
    width: 220,
    backgroundColor: '#1e293b',
    borderRight: '1px solid #334155',
    display: 'flex',
    flexDirection: 'column',
    padding: '16px 0',
  },
  logo: {
    padding: '0 20px 20px',
    borderBottom: '1px solid #334155',
  },
  logoText: { color: '#3b82f6', fontSize: 20, margin: 0 },
  logoSubtext: { color: '#94a3b8', fontSize: 12 },
  nav: { flex: 1, padding: '12px 8px', display: 'flex', flexDirection: 'column', gap: 4 },
  navItem: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '10px 12px',
    borderRadius: 6,
    color: '#94a3b8',
    textDecoration: 'none',
    fontSize: 14,
    transition: 'all 0.15s',
  },
  navItemActive: {
    backgroundColor: '#334155',
    color: '#f1f5f9',
  },
  navIcon: {
    width: 24,
    height: 24,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: '#475569',
    borderRadius: 4,
    fontSize: 12,
    fontWeight: 'bold' as const,
    color: '#3b82f6',
  },
  footer: { padding: '12px 20px', borderTop: '1px solid #334155' },
  footerText: { color: '#475569', fontSize: 11 },
};
