import { Link, useLocation } from 'react-router-dom';
import { useTheme } from '../../theme/ThemeProvider';
import { useUIStore } from '../../stores/uiStore';
import { CookerMark, Icon, type IconName } from '../ui/Icon';
import { SectionLabel, StatusDot } from '../ui/atoms';

interface NavItem {
  path: string;
  label: string;
  icon: IconName;
  proOnly?: boolean;
}

const navItems: NavItem[] = [
  { path: '/apps', label: 'Apps', icon: 'apps' },
  { path: '/pipelines', label: 'Pipelines', icon: 'pipe' },
  { path: '/registry', label: 'Registry', icon: 'layers', proOnly: true },
  { path: '/docker', label: 'Docker', icon: 'box', proOnly: true },
  { path: '/docker/compose', label: 'Compose', icon: 'compose' },
  { path: '/kubernetes', label: 'Clusters', icon: 'servers', proOnly: true },
  { path: '/hosts', label: 'Hosts', icon: 'servers', proOnly: true },
  { path: '/environments', label: 'Environments', icon: 'flask' },
  { path: '/settings', label: 'Settings', icon: 'cog', proOnly: true },
];

const recent = [
  { name: 'checkout-api', tone: 'good' as const },
  { name: 'billing-worker', tone: 'ember' as const },
  { name: 'marketing-site', tone: 'warn' as const },
];

export default function Sidebar() {
  const t = useTheme();
  const location = useLocation();
  const mode = useUIStore((s) => s.mode);
  const visible = navItems.filter((i) => mode === 'pro' || !i.proOnly);

  return (
    <aside
      style={{
        width: 232,
        background: t.surfaceAlt,
        borderRight: `1px solid ${t.line}`,
        display: 'flex',
        flexDirection: 'column',
        flexShrink: 0,
      }}
    >
      {/* Logo */}
      <div style={{ padding: '20px 22px 18px', borderBottom: `1px solid ${t.line}` }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <CookerMark color={t.accent} size={28} />
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <span
              style={{
                fontFamily: t.serif,
                fontSize: 22,
                fontWeight: 600,
                color: t.text,
                lineHeight: 1,
                letterSpacing: -0.4,
              }}
            >
              Cooker
            </span>
            <span
              style={{
                fontFamily: t.mono,
                fontSize: 9.5,
                letterSpacing: 1.6,
                color: t.textMute,
                textTransform: 'uppercase',
                marginTop: 3,
              }}
            >
              build · ship · run
            </span>
          </div>
        </div>
      </div>

      {/* Workspace */}
      <div style={{ padding: '14px 16px 6px' }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            padding: '10px 12px',
            background: t.surface,
            border: `1px solid ${t.line}`,
            borderRadius: 8,
          }}
        >
          <div
            style={{
              width: 22,
              height: 22,
              borderRadius: 6,
              background: t.accent,
              color: '#FFF8EE',
              display: 'grid',
              placeItems: 'center',
              fontFamily: t.mono,
              fontSize: 11,
              fontWeight: 700,
            }}
          >
            CK
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 12.5, color: t.text, fontWeight: 600, lineHeight: 1.1 }}>
              Cooker
            </div>
            <div style={{ fontFamily: t.mono, fontSize: 10, color: t.textMute, marginTop: 2 }}>
              {mode === 'pro' ? 'pro · workspace' : 'simple · workspace'}
            </div>
          </div>
          <Icon
            name="arrow"
            size={12}
            style={{ color: t.textMute, transform: 'rotate(90deg)' }}
          />
        </div>
      </div>

      {/* Nav */}
      <nav
        style={{
          flex: 1,
          padding: '10px 12px',
          display: 'flex',
          flexDirection: 'column',
          gap: 2,
          overflow: 'auto',
        }}
      >
        {visible.map((item) => {
          const isActive =
            item.path === '/'
              ? location.pathname === '/'
              : location.pathname === item.path ||
                location.pathname.startsWith(item.path + '/');
          return (
            <Link
              key={item.path}
              to={item.path}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                padding: '8px 10px',
                borderRadius: 7,
                background: isActive ? t.surface : 'transparent',
                color: isActive ? t.text : t.textSoft,
                border: `1px solid ${isActive ? t.line : 'transparent'}`,
                boxShadow: isActive ? `inset 2px 0 0 ${t.accent}` : 'none',
                textDecoration: 'none',
              }}
            >
              <span style={{ color: isActive ? t.accent : t.textMute, display: 'flex' }}>
                <Icon name={item.icon} size={15} />
              </span>
              <span style={{ flex: 1, fontSize: 13, fontWeight: isActive ? 600 : 500 }}>
                {item.label}
              </span>
            </Link>
          );
        })}

        <div style={{ height: 14 }} />
        <SectionLabel style={{ padding: '0 6px' }}>Recent</SectionLabel>
        {recent.map((r) => (
          <div
            key={r.name}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '6px 10px 6px 12px',
              color: t.textSoft,
              fontSize: 12.5,
              cursor: 'pointer',
              borderRadius: 6,
            }}
          >
            <StatusDot tone={r.tone} pulse={r.tone === 'ember'} />
            <span style={{ fontFamily: t.mono, fontSize: 11.5 }}>{r.name}</span>
          </div>
        ))}
      </nav>

      {/* Footer */}
      <div
        style={{
          padding: '12px 16px',
          borderTop: `1px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 10,
        }}
      >
        <div
          style={{
            width: 28,
            height: 28,
            borderRadius: 999,
            background: t.accentDeep,
            color: '#FFF8EE',
            display: 'grid',
            placeItems: 'center',
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 11,
          }}
        >
          OP
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 12, fontWeight: 600, color: t.text, lineHeight: 1 }}>
            Operator
          </div>
          <div style={{ fontFamily: t.mono, fontSize: 10, color: t.textMute, marginTop: 3 }}>
            signed in
          </div>
        </div>
        <Icon name="cog" size={14} style={{ color: t.textMute, cursor: 'pointer' }} />
      </div>
    </aside>
  );
}
