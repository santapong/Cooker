/* Cosmic Cooker — reusable UI atoms + app shell */

// ----------------------------------------------------------------- TopBar
function TopBar({ theme, crumbs }) {
  return (
    <header style={{ height: 54, flexShrink: 0, position: 'relative', zIndex: 5,
      display: 'flex', alignItems: 'center', gap: 14, padding: '0 20px',
      borderBottom: `1px solid ${theme.line}`, background: theme.panelGlass,
      backdropFilter: 'blur(12px)', WebkitBackdropFilter: 'blur(12px)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12.5, color: theme.textSoft }}>
        {crumbs.map((c, i) => (
          <span key={i} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {i > 0 && <span style={{ color: theme.textFaint, fontFamily: theme.mono, fontSize: 11 }}>/</span>}
            <span style={{ color: i === crumbs.length - 1 ? theme.text : theme.textSoft,
              fontWeight: i === crumbs.length - 1 ? 600 : 500,
              fontFamily: c.mono ? theme.mono : theme.sans }}>{c.label}</span>
          </span>
        ))}
      </div>
      <div style={{ flex: 1 }} />
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, width: 264, color: theme.textMute,
        background: hexA(theme.surfaceSolid, theme.mode === 'dark' ? 0.5 : 0.6),
        border: `1px solid ${theme.line}`, borderRadius: 9, padding: '7px 11px' }}>
        <span style={{ fontSize: 13 }}>⌕</span>
        <span style={{ fontSize: 12.5, flex: 1 }}>Find an app, run, image…</span>
        <span style={{ fontFamily: theme.mono, fontSize: 10, padding: '2px 6px', borderRadius: 5,
          border: `1px solid ${theme.line}`, color: theme.textSoft }}>⌘K</span>
      </div>
      <div style={{ display: 'flex', padding: 3, borderRadius: 8, gap: 2,
        background: hexA(theme.surfaceSolid, 0.5), border: `1px solid ${theme.line}` }}>
        {['simple', 'pro'].map((m) => (
          <span key={m} style={{ padding: '4px 11px', borderRadius: 5, fontSize: 11, fontWeight: 600,
            letterSpacing: 0.4, textTransform: 'uppercase', cursor: 'pointer', fontFamily: theme.sans,
            background: m === 'pro' ? hexA(theme.violet, 0.18) : 'transparent',
            color: m === 'pro' ? theme.violet : theme.textMute }}>{m}</span>
        ))}
      </div>
      <span style={{ width: 32, height: 32, display: 'grid', placeItems: 'center', borderRadius: 8,
        border: `1px solid ${theme.line}`, color: theme.textSoft, cursor: 'pointer', fontSize: 14 }}>
        {theme.mode === 'dark' ? '☾' : '☀'}</span>
      <span style={{ position: 'relative', color: theme.textSoft, cursor: 'pointer', fontSize: 16 }}>
        ◔
        <span style={{ position: 'absolute', top: -1, right: -2, width: 7, height: 7, borderRadius: 999,
          background: theme.ember, border: `1.5px solid ${theme.bg}` }} />
      </span>
    </header>
  );
}

// ----------------------------------------------------------------- Atoms
function CPill({ children, color, theme, solid }) {
  const c = color || theme.textMute;
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, fontFamily: theme.mono,
      fontSize: 10, letterSpacing: 0.5, textTransform: 'uppercase', padding: '3px 9px', borderRadius: 999,
      color: solid ? '#fff' : c, background: solid ? c : hexA(c, 0.13),
      border: `1px solid ${hexA(c, solid ? 0.6 : 0.32)}`, whiteSpace: 'nowrap' }}>{children}</span>
  );
}

function StatusDot({ color, pulse, theme, size = 8 }) {
  return (
    <span style={{ width: size, height: size, borderRadius: 999, background: color, flexShrink: 0,
      boxShadow: pulse ? `0 0 8px ${color}` : 'none',
      animation: pulse ? 'ccPulse 1.6s ease-out infinite' : 'none' }} />
  );
}

function CBtn({ children, kind = 'secondary', theme, style }) {
  const map = {
    primary: { background: `linear-gradient(135deg, ${theme.violet}, ${theme.violetGlow})`, color: '#fff',
      border: `1px solid ${hexA(theme.violetGlow, 0.6)}`, boxShadow: `0 6px 18px ${hexA(theme.violetGlow, 0.38)}` },
    ink: { background: theme.mode === 'dark' ? theme.text : '#1a1730', color: theme.bg, border: `1px solid ${theme.text}` },
    secondary: { background: hexA(theme.surfaceSolid, 0.6), color: theme.text, border: `1px solid ${theme.line}` },
    ghost: { background: 'transparent', color: theme.textSoft, border: `1px solid transparent` },
    danger: { background: hexA(theme.bad, 0.13), color: theme.bad, border: `1px solid ${hexA(theme.bad, 0.4)}` },
  };
  return (
    <button style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: 7,
      padding: '8px 14px', borderRadius: 9, fontSize: 12.5, fontFamily: theme.sans, fontWeight: 600,
      cursor: 'pointer', whiteSpace: 'nowrap', ...map[kind], ...style }}>{children}</button>
  );
}

function GlassCard({ children, theme, pad = 20, style, accent }) {
  return (
    <div style={{ background: theme.surface, backdropFilter: 'blur(10px)', WebkitBackdropFilter: 'blur(10px)',
      border: `1px solid ${theme.line}`, borderRadius: 14, padding: pad,
      borderLeft: accent ? `3px solid ${accent}` : `1px solid ${theme.line}`,
      boxShadow: `0 10px 30px ${hexA('#000', theme.mode === 'dark' ? 0.32 : 0.08)}`, ...style }}>
      {children}
    </div>
  );
}

function SLabel({ children, theme, style }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 10, ...style }}>
      <span style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 1.6, textTransform: 'uppercase',
        color: theme.textMute }}>{children}</span>
      <span style={{ flex: 1, height: 1, background: theme.line }} />
    </div>
  );
}

function CPageHeader({ eyebrow, title, subtitle, actions, theme }) {
  return (
    <div style={{ display: 'flex', alignItems: 'flex-end', gap: 24, marginBottom: 24 }}>
      <div style={{ minWidth: 0 }}>
        {eyebrow && <div style={{ fontFamily: theme.mono, fontSize: 10.5, letterSpacing: 1.8,
          textTransform: 'uppercase', color: theme.violet, marginBottom: 10 }}>{eyebrow}</div>}
        <h1 style={{ fontFamily: theme.display, fontSize: 34, lineHeight: 1.05, fontWeight: 600,
          color: theme.text, letterSpacing: -0.8, margin: 0 }}>{title}</h1>
        {subtitle && <p style={{ fontSize: 13.5, color: theme.textSoft, marginTop: 12, lineHeight: 1.6,
          maxWidth: 680 }}>{subtitle}</p>}
      </div>
      <div style={{ flex: 1 }} />
      {actions && <div style={{ display: 'flex', gap: 10 }}>{actions}</div>}
    </div>
  );
}

function HealthBar({ value, theme }) {
  const c = value > 95 ? theme.good : value > 80 ? theme.warn : theme.bad;
  return (
    <div style={{ width: 92, height: 5, borderRadius: 3, overflow: 'hidden', background: theme.line }}>
      <div style={{ width: value + '%', height: '100%', borderRadius: 3, background: c,
        boxShadow: `0 0 8px ${hexA(c, 0.6)}` }} />
    </div>
  );
}

// ----------------------------------------------------------------- App shell
function AppShell({ theme, active, crumbs, children, scroll = true }) {
  return (
    <div style={{ width: '100%', height: '100%', display: 'flex', position: 'relative', overflow: 'hidden',
      fontFamily: theme.sans, background: theme.bg, color: theme.text }}>
      <CosmicSidebar theme={theme} active={active} />
      <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
        <TopBar theme={theme} crumbs={crumbs} />
        <div style={{ flex: 1, minHeight: 0, position: 'relative', overflow: 'hidden',
          background: `radial-gradient(120% 90% at 85% -10%, ${hexA(theme.violetGlow, theme.mode === 'dark' ? 0.16 : 0.08)} 0%, transparent 50%),
                       radial-gradient(110% 90% at 0% 110%, ${hexA(theme.cyan, theme.mode === 'dark' ? 0.1 : 0.06)} 0%, transparent 50%),
                       linear-gradient(${theme.canvasTop}, ${theme.canvasBot})` }}>
          <Starfield theme={theme} seed={17} density={theme.mode === 'dark' ? 64 : 16} nebula={false} />
          <div style={{ position: 'relative', height: '100%', overflow: scroll ? 'auto' : 'hidden' }}>
            {children}
          </div>
        </div>
      </div>
    </div>
  );
}

function Tabs({ theme, tabs, active }) {
  return (
    <div style={{ display: 'flex', gap: 2, padding: 3, borderRadius: 9, alignSelf: 'flex-start',
      background: hexA(theme.surfaceSolid, 0.5), border: `1px solid ${theme.line}` }}>
      {tabs.map((tb) => (
        <span key={tb} style={{ padding: '6px 13px', borderRadius: 6, fontSize: 12, fontWeight: 600,
          fontFamily: theme.sans, cursor: 'pointer', whiteSpace: 'nowrap',
          background: tb === active ? hexA(theme.violet, 0.18) : 'transparent',
          color: tb === active ? theme.violet : theme.textMute }}>{tb}</span>
      ))}
    </div>
  );
}

function Stat({ theme, label, value, sub, color }) {
  return (
    <GlassCard theme={theme} pad={16} accent={color}>
      <div style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 1.2, textTransform: 'uppercase',
        color: theme.textMute }}>{label}</div>
      <div style={{ fontFamily: theme.display, fontSize: 26, fontWeight: 600, color: theme.text,
        marginTop: 6, letterSpacing: -0.5 }}>{value}</div>
      {sub && <div style={{ fontSize: 11.5, color: color || theme.textMute, marginTop: 3 }}>{sub}</div>}
    </GlassCard>
  );
}

function KV({ theme, k, v, color }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <span style={{ fontFamily: theme.mono, fontSize: 9.5, letterSpacing: 1.1, textTransform: 'uppercase',
        color: theme.textMute }}>{k}</span>
      <span style={{ fontFamily: theme.mono, fontSize: 12, color: color || theme.text, wordBreak: 'break-all' }}>{v}</span>
    </div>
  );
}

Object.assign(window, { TopBar, CPill, StatusDot, CBtn, GlassCard, SLabel, CPageHeader, HealthBar, AppShell, Tabs, Stat, KV });
