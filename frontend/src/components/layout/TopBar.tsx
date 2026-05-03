import { useLocation, useNavigate, useParams } from 'react-router-dom';
import type { ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { useUIStore } from '../../stores/uiStore';
import { hexA } from '../../theme/tokens';
import { Icon } from '../ui/Icon';
import { KBD, Pill, type Tone } from '../ui/atoms';

export interface Crumb {
  label: string;
  mono?: boolean;
  badge?: string;
  badgeTone?: Tone;
  to?: string;
}

interface Props {
  crumbs?: Crumb[];
  right?: ReactNode;
}

export default function TopBar({ crumbs, right }: Props) {
  const t = useTheme();
  const navigate = useNavigate();
  const location = useLocation();
  const params = useParams();
  const mode = useUIStore((s) => s.mode);
  const setMode = useUIStore((s) => s.setMode);
  const themeMode = useUIStore((s) => s.themeMode);
  const setThemeMode = useUIStore((s) => s.setThemeMode);

  const resolvedCrumbs = crumbs ?? deriveCrumbs(location.pathname, params);

  return (
    <header
      style={{
        height: 56,
        display: 'flex',
        alignItems: 'center',
        borderBottom: `1px solid ${t.line}`,
        background: t.bg,
        padding: '0 22px',
        gap: 16,
        flexShrink: 0,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, color: t.textSoft, fontSize: 13, minWidth: 0 }}>
        {resolvedCrumbs.map((c, i) => (
          <span key={i} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {i > 0 && (
              <span style={{ color: t.textMute, fontFamily: t.mono, fontSize: 11 }}>/</span>
            )}
            <span
              onClick={c.to ? () => navigate(c.to!) : undefined}
              style={{
                color: i === resolvedCrumbs.length - 1 ? t.text : t.textSoft,
                fontWeight: i === resolvedCrumbs.length - 1 ? 600 : 500,
                fontFamily: c.mono ? t.mono : t.sans,
                fontSize: c.mono ? 12 : 13,
                cursor: c.to ? 'pointer' : 'default',
                whiteSpace: 'nowrap',
              }}
            >
              {c.label}
            </span>
            {c.badge && <Pill tone={c.badgeTone || 'neutral'}>{c.badge}</Pill>}
          </span>
        ))}
      </div>

      <div style={{ flex: 1 }} />

      {/* Search */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          background: t.surface,
          border: `1px solid ${t.line}`,
          borderRadius: 8,
          padding: '6px 10px',
          width: 280,
          color: t.textMute,
        }}
      >
        <Icon name="search" size={14} />
        <span style={{ fontSize: 12.5, flex: 1 }}>Find an app, run, image…</span>
        <KBD>⌘K</KBD>
      </div>

      {/* Mode switch */}
      <div
        style={{
          display: 'flex',
          padding: 3,
          background: t.surfaceAlt,
          border: `1px solid ${t.line}`,
          borderRadius: 8,
        }}
      >
        {(['simple', 'pro'] as const).map((m) => (
          <button
            key={m}
            onClick={() => setMode(m)}
            style={{
              background: mode === m ? t.surface : 'transparent',
              color: mode === m ? t.text : t.textMute,
              fontFamily: t.sans,
              fontSize: 11.5,
              fontWeight: 600,
              letterSpacing: 0.4,
              textTransform: 'uppercase',
              border: 'none',
              padding: '5px 12px',
              borderRadius: 5,
              cursor: 'pointer',
              boxShadow: mode === m ? `0 1px 2px ${hexA('#000', 0.06)}` : 'none',
            }}
          >
            {m}
          </button>
        ))}
      </div>

      {right}

      {/* Theme toggle */}
      <button
        onClick={() => setThemeMode(themeMode === 'dark' ? 'light' : 'dark')}
        title={themeMode === 'dark' ? 'Switch to paper' : 'Switch to coal'}
        style={{
          background: 'transparent',
          color: t.textSoft,
          border: `1px solid ${t.line}`,
          borderRadius: 8,
          width: 32,
          height: 32,
          display: 'grid',
          placeItems: 'center',
          cursor: 'pointer',
        }}
      >
        <Icon name={themeMode === 'dark' ? 'sun' : 'moon'} size={14} />
      </button>

      {/* Bell */}
      <div style={{ position: 'relative', color: t.textSoft, cursor: 'pointer' }}>
        <Icon name="bell" size={16} />
        <span
          style={{
            position: 'absolute',
            top: -3,
            right: -3,
            width: 7,
            height: 7,
            background: t.accent,
            borderRadius: 999,
            border: `1.5px solid ${t.bg}`,
          }}
        />
      </div>
    </header>
  );
}

function deriveCrumbs(pathname: string, params: Record<string, string | undefined>): Crumb[] {
  const seg = pathname.split('/').filter(Boolean);
  if (seg.length === 0) return [{ label: 'Cooker' }, { label: 'Apps' }];
  const head: Crumb = { label: 'Cooker', to: '/' };

  if (seg[0] === 'apps') {
    const c: Crumb[] = [head, { label: 'Apps', to: '/apps' }];
    if (params.id) c.push({ label: params.id, mono: true });
    return c;
  }
  if (seg[0] === 'pipelines') {
    const c: Crumb[] = [head, { label: 'Pipelines', to: '/pipelines' }];
    if (seg[1]) c.push({ label: seg[1].slice(0, 8), mono: true });
    if (seg[2] === 'edit') c.push({ label: 'edit', mono: true });
    return c;
  }
  if (seg[0] === 'docker') {
    return seg[1] === 'compose'
      ? [head, { label: 'Docker', to: '/docker' }, { label: 'Compose' }]
      : [head, { label: 'Docker' }];
  }
  if (seg[0] === 'kubernetes') return [head, { label: 'Clusters' }];
  if (seg[0] === 'environments') return [head, { label: 'Environments' }];
  return [head, { label: seg[0] }];
}
