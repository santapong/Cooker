import { Link, useLocation } from 'react-router-dom';
import { useTheme } from '../../theme/ThemeProvider';
import { useUIStore } from '../../stores/uiStore';
import { hexA } from '../../theme/tokens';
import { CookerMark, Icon, type IconName } from '../ui/Icon';
import { SectionLabel, StatusDot } from '../ui/atoms';
import { Starfield } from '../ui/Starfield';

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
  { path: '/cloud', label: 'Cloud', icon: 'box', proOnly: true },
  { path: '/hosts', label: 'Hosts', icon: 'servers', proOnly: true },
  { path: '/environments', label: 'Environments', icon: 'flask' },
  { path: '/analytics', label: 'Insights', icon: 'spark', proOnly: true },
  { path: '/admin/templates', label: 'Templates', icon: 'spark' },
  { path: '/admin/schedules', label: 'Schedules', icon: 'pause', proOnly: true },
  { path: '/admin/notifications', label: 'Notifications', icon: 'bell', proOnly: true },
  { path: '/admin/audit', label: 'Audit log', icon: 'layers', proOnly: true },
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
        position: 'relative',
        overflow: 'hidden',
      }}
    >
      {/* sparse, non-drifting starfield behind the chrome */}
      <Starfield seed={7} density={36} nebula={false} />

      {/* Brand — ringed-planet mark */}
      <div
        style={{
          position: 'relative',
          padding: '20px 20px 17px',
          borderBottom: `1px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 11,
        }}
      >
        <CookerMark size={30} />
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span
            style={{
              fontFamily: t.display,
              fontSize: 21,
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
              fontSize: 9,
              letterSpacing: 2,
              color: t.textMute,
              textTransform: 'uppercase',
              marginTop: 4,
            }}
          >
            build · ship · orbit
          </span>
        </div>
      </div>

      {/* Workspace */}
      <div style={{ position: 'relative', padding: '13px 14px 5px' }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            padding: '9px 11px',
            background: t.surface,
            border: `1px solid ${t.line}`,
            borderRadius: 9,
          }}
        >
          <div
            style={{
              width: 22,
              height: 22,
              borderRadius: 7,
              background: `linear-gradient(135deg, ${t.violet}, ${t.cyan})`,
              color: '#fff',
              display: 'grid',
              placeItems: 'center',
              fontFamily: t.mono,
              fontSize: 10.5,
              fontWeight: 700,
            }}
          >
            CK
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 12.5, color: t.text, fontWeight: 600, lineHeight: 1.1 }}>
              Cooker
            </div>
            <div style={{ fontFamily: t.mono, fontSize: 9.5, color: t.textMute, marginTop: 2 }}>
              {mode === 'pro' ? 'pro · galaxy' : 'simple · galaxy'}
            </div>
          </div>
          <Icon name="arrow" size={12} style={{ color: t.textMute, transform: 'rotate(90deg)' }} />
        </div>
      </div>

      {/* Nav */}
      <nav
        style={{
          position: 'relative',
          flex: 1,
          padding: '9px 12px',
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
                gap: 11,
                padding: '8px 11px',
                borderRadius: 8,
                background: isActive ? hexA(t.violet, t.mode === 'dark' ? 0.16 : 0.1) : 'transparent',
                color: isActive ? t.text : t.textSoft,
                border: `1px solid ${isActive ? hexA(t.violet, 0.4) : 'transparent'}`,
                boxShadow: isActive
                  ? `inset 2px 0 0 ${t.violet}, 0 0 18px ${hexA(t.violetGlow, 0.18)}`
                  : 'none',
                textDecoration: 'none',
              }}
            >
              <span
                style={{
                  color: isActive ? t.violet : t.textMute,
                  display: 'flex',
                  width: 16,
                  justifyContent: 'center',
                }}
              >
                <Icon name={item.icon} size={15} />
              </span>
              <span style={{ flex: 1, fontSize: 13, fontWeight: isActive ? 600 : 500 }}>
                {item.label}
              </span>
              {isActive && (
                <span
                  style={{
                    width: 5,
                    height: 5,
                    borderRadius: 999,
                    background: t.violet,
                    boxShadow: `0 0 7px ${t.violet}`,
                  }}
                />
              )}
            </Link>
          );
        })}

        <div style={{ height: 12 }} />
        <SectionLabel style={{ padding: '0 6px' }}>Constellation</SectionLabel>
        {recent.map((r) => (
          <div
            key={r.name}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              padding: '6px 11px',
              color: t.textSoft,
              fontSize: 12.5,
              cursor: 'pointer',
              borderRadius: 7,
            }}
          >
            <StatusDot tone={r.tone} pulse={r.tone === 'ember'} size={7} />
            <span style={{ fontFamily: t.mono, fontSize: 11.5 }}>{r.name}</span>
          </div>
        ))}
      </nav>

      {/* Footer */}
      <div
        style={{
          position: 'relative',
          padding: '11px 15px',
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
            background: `linear-gradient(135deg, ${t.ember}, ${t.emberDeep})`,
            color: '#fff',
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
          <div style={{ fontSize: 12, fontWeight: 600, color: t.text, lineHeight: 1 }}>Operator</div>
          <div style={{ fontFamily: t.mono, fontSize: 9.5, color: t.textMute, marginTop: 3 }}>
            signed in
          </div>
        </div>
        <Icon name="cog" size={14} style={{ color: t.textMute, cursor: 'pointer' }} />
      </div>
    </aside>
  );
}
