/* Cosmic Cooker — app chrome (sidebar, header, palette, inspector, minimap) */

// ----------------------------------------------------------------- Logo mark
function CookerMark({ theme, size = 30 }) {
  // a small ringed planet — the brand becomes a world with an orbit
  return (
    <div style={{ position: 'relative', width: size, height: size, flexShrink: 0 }}>
      <div style={{ position: 'absolute', inset: -size * 0.18, borderRadius: 999,
        background: `radial-gradient(circle, ${hexA(theme.violetGlow, 0.5)} 0%, transparent 70%)` }} />
      <div style={{ position: 'absolute', inset: size * 0.14, borderRadius: 999,
        background: `radial-gradient(circle at 34% 30%, ${theme.cyan}, ${theme.violet} 70%)`,
        boxShadow: `inset -2px -3px 5px ${hexA('#000', 0.4)}, 0 0 ${size * 0.5}px ${hexA(theme.violetGlow, 0.6)}` }} />
      <div style={{ position: 'absolute', left: -size * 0.06, top: '50%', width: size * 1.12, height: size * 0.42,
        marginTop: -size * 0.21, borderRadius: '50%', transform: 'rotate(-22deg)',
        border: `2px solid ${hexA(theme.ember, 0.85)}`, borderTopColor: hexA(theme.ember, 0.2),
        borderLeftColor: hexA(theme.ember, 0.3) }} />
    </div>
  );
}

const NAV = [
  { label: 'Apps', ch: '◴', },
  { label: 'Pipelines', ch: '✦' },
  { label: 'Registry', ch: '⬡' },
  { label: 'Clusters', ch: '◍' },
  { label: 'Environments', ch: '⬢' },
  { label: 'Templates', ch: '✺' },
  { label: 'Schedules', ch: '◷' },
  { label: 'Settings', ch: '⚙' },
];
const RECENT = [
  { name: 'checkout-api', tone: 'good' },
  { name: 'billing-worker', tone: 'ember' },
  { name: 'marketing-site', tone: 'warn' },
];

function CosmicSidebar({ theme, active = 'Pipelines' }) {
  const toneColor = { good: theme.good, ember: theme.ember, warn: theme.warn };
  return (
    <aside style={{ width: 232, flexShrink: 0, position: 'relative',
      borderRight: `1px solid ${theme.line}`, background: theme.surfaceAlt,
      display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <Starfield theme={theme} seed={7} density={36} nebula={false} />
      {/* brand */}
      <div style={{ position: 'relative', padding: '20px 20px 17px', borderBottom: `1px solid ${theme.line}`,
        display: 'flex', alignItems: 'center', gap: 11 }}>
        <CookerMark theme={theme} size={30} />
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ fontFamily: theme.display, fontSize: 21, fontWeight: 600, color: theme.text,
            lineHeight: 1, letterSpacing: -0.4 }}>Cooker</span>
          <span style={{ fontFamily: theme.mono, fontSize: 9, letterSpacing: 2, color: theme.textMute,
            textTransform: 'uppercase', marginTop: 4 }}>build · ship · orbit</span>
        </div>
      </div>
      {/* workspace */}
      <div style={{ position: 'relative', padding: '13px 14px 5px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '9px 11px',
          background: theme.surface, border: `1px solid ${theme.line}`, borderRadius: 9 }}>
          <div style={{ width: 22, height: 22, borderRadius: 7, display: 'grid', placeItems: 'center',
            background: `linear-gradient(135deg, ${theme.violet}, ${theme.cyan})`, color: '#fff',
            fontFamily: theme.mono, fontSize: 10.5, fontWeight: 700 }}>CK</div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 12.5, color: theme.text, fontWeight: 600, lineHeight: 1.1 }}>Cooker</div>
            <div style={{ fontFamily: theme.mono, fontSize: 9.5, color: theme.textMute, marginTop: 2 }}>pro · galaxy</div>
          </div>
          <span style={{ color: theme.textMute, fontSize: 12 }}>⌄</span>
        </div>
      </div>
      {/* nav */}
      <nav style={{ position: 'relative', flex: 1, padding: '9px 12px', display: 'flex',
        flexDirection: 'column', gap: 2, overflow: 'auto' }}>
        {NAV.map((nav) => { const it = { ...nav, active: nav.label === active }; return (
          <div key={it.label} style={{ display: 'flex', alignItems: 'center', gap: 11, padding: '8px 11px',
            borderRadius: 8, cursor: 'pointer',
            background: it.active ? hexA(theme.violet, theme.mode === 'dark' ? 0.16 : 0.1) : 'transparent',
            border: `1px solid ${it.active ? hexA(theme.violet, 0.4) : 'transparent'}`,
            boxShadow: it.active ? `inset 2px 0 0 ${theme.violet}, 0 0 18px ${hexA(theme.violetGlow, 0.18)}` : 'none',
            color: it.active ? theme.text : theme.textSoft }}>
            <span style={{ width: 16, textAlign: 'center', fontSize: 13,
              color: it.active ? theme.violet : theme.textMute }}>{it.ch}</span>
            <span style={{ flex: 1, fontSize: 13, fontWeight: it.active ? 600 : 500 }}>{it.label}</span>
            {it.active && <span style={{ width: 5, height: 5, borderRadius: 999, background: theme.violet,
              boxShadow: `0 0 7px ${theme.violet}` }} />}
          </div>
        ); })}
        <div style={{ height: 12 }} />
        <div style={{ display: 'flex', alignItems: 'center', gap: 9, padding: '0 8px 6px' }}>
          <span style={{ fontFamily: theme.mono, fontSize: 9.5, letterSpacing: 1.6, textTransform: 'uppercase',
            color: theme.textMute }}>Constellation</span>
          <span style={{ flex: 1, height: 1, background: theme.line }} />
        </div>
        {RECENT.map((r) => (
          <div key={r.name} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '6px 11px',
            color: theme.textSoft, cursor: 'pointer', borderRadius: 7 }}>
            <span style={{ width: 7, height: 7, borderRadius: 999, background: toneColor[r.tone],
              boxShadow: r.tone === 'ember' ? `0 0 7px ${theme.ember}` : 'none',
              animation: r.tone === 'ember' ? 'ccPulse 1.6s ease-out infinite' : 'none' }} />
            <span style={{ fontFamily: theme.mono, fontSize: 11.5 }}>{r.name}</span>
          </div>
        ))}
      </nav>
      {/* footer */}
      <div style={{ position: 'relative', padding: '11px 15px', borderTop: `1px solid ${theme.line}`,
        display: 'flex', alignItems: 'center', gap: 10 }}>
        <div style={{ width: 28, height: 28, borderRadius: 999, display: 'grid', placeItems: 'center',
          background: `linear-gradient(135deg, ${theme.ember}, ${theme.emberDeep})`, color: '#fff',
          fontFamily: theme.mono, fontWeight: 700, fontSize: 11 }}>OP</div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 12, fontWeight: 600, color: theme.text, lineHeight: 1 }}>Operator</div>
          <div style={{ fontFamily: theme.mono, fontSize: 9.5, color: theme.textMute, marginTop: 3 }}>signed in</div>
        </div>
        <span style={{ color: theme.textMute, fontSize: 13 }}>⚙</span>
      </div>
    </aside>
  );
}

// ----------------------------------------------------------------- Editor header
function EditorHeader({ theme, name = 'checkout-api', steps = 6, edges = 6 }) {
  const Pill = ({ children, color }) => (
    <span style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 0.5, textTransform: 'uppercase',
      padding: '3px 9px', borderRadius: 999, color: color || theme.textSoft,
      background: hexA(color || theme.textMute, 0.12), border: `1px solid ${hexA(color || theme.textMute, 0.3)}` }}>{children}</span>
  );
  return (
    <div style={{ height: 54, flexShrink: 0, position: 'relative', zIndex: 3,
      borderBottom: `1px solid ${theme.line}`, background: theme.panelGlass,
      backdropFilter: 'blur(12px)', WebkitBackdropFilter: 'blur(12px)',
      display: 'flex', alignItems: 'center', gap: 13, padding: '0 20px' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, color: theme.textSoft, fontSize: 12.5 }}>
        <span>Cooker</span><span style={{ color: theme.textFaint }}>/</span>
        <span>Pipelines</span><span style={{ color: theme.textFaint }}>/</span>
        <span style={{ fontFamily: theme.mono, color: theme.text, fontWeight: 600 }}>{name}</span>
      </div>
      <PlanetOrb kind="source" size={18} theme={theme} />
      <span style={{ fontFamily: theme.display, fontSize: 16, fontWeight: 600, color: theme.text, letterSpacing: -0.2 }}>
        Star Map</span>
      <Pill color={theme.violet}>{steps} planets</Pill>
      <Pill color={theme.cyan}>{edges} links</Pill>
      <div style={{ flex: 1 }} />
      <div style={{ display: 'flex', alignItems: 'center', gap: 7, padding: '5px 11px', borderRadius: 999,
        background: hexA(theme.ember, 0.12), border: `1px solid ${hexA(theme.ember, 0.35)}` }}>
        <span style={{ width: 7, height: 7, borderRadius: 999, background: theme.ember,
          animation: 'ccPulse 1.6s ease-out infinite' }} />
        <span style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.ember, letterSpacing: 0.5 }}>
          run #4b2f · live</span>
      </div>
      <span style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute }}>updated 2m ago</span>
    </div>
  );
}

// ----------------------------------------------------------------- Palette
const STAGE_DEFS = [
  { type: 'build', label: 'Build image', desc: 'Kaniko / Buildx' },
  { type: 'test', label: 'Run tests', desc: 'go / npm / pytest' },
  { type: 'push', label: 'Push to registry', desc: 'OCI distribution' },
  { type: 'deploy', label: 'Deploy', desc: 'Kubernetes' },
  { type: 'approval', label: 'Approval gate', desc: 'human in the loop' },
  { type: 'custom', label: 'Custom shell', desc: 'any container' },
];

function Palette({ theme }) {
  const Btn = ({ children, kind }) => {
    const map = {
      primary: { background: `linear-gradient(135deg, ${theme.violet}, ${theme.violetGlow})`, color: '#fff',
        border: `1px solid ${hexA(theme.violetGlow, 0.6)}`, boxShadow: `0 6px 18px ${hexA(theme.violetGlow, 0.4)}` },
      ink: { background: theme.mode === 'dark' ? theme.text : '#1a1730', color: theme.bg, border: `1px solid ${theme.text}` },
      secondary: { background: theme.surface, color: theme.text, border: `1px solid ${theme.line}` },
    };
    return <button style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 7,
      padding: '9px 12px', borderRadius: 9, fontSize: 12.5, fontFamily: theme.sans, fontWeight: 600,
      cursor: 'pointer', ...map[kind] }}>{children}</button>;
  };
  return (
    <aside style={{ width: 212, flexShrink: 0, position: 'relative', zIndex: 2,
      borderRight: `1px solid ${theme.line}`, background: theme.panelGlass,
      backdropFilter: 'blur(12px)', WebkitBackdropFilter: 'blur(12px)',
      padding: '16px 13px', display: 'flex', flexDirection: 'column', gap: 7, overflow: 'hidden' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 9 }}>
        <span style={{ fontFamily: theme.mono, fontSize: 9.5, letterSpacing: 1.6, textTransform: 'uppercase',
          color: theme.textMute }}>Launch a planet</span>
        <span style={{ flex: 1, height: 1, background: theme.line }} />
      </div>
      {STAGE_DEFS.map((s) => (
        <div key={s.type} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 10px',
          border: `1px solid ${theme.line}`, background: hexA(theme.surfaceSolid, theme.mode === 'dark' ? 0.5 : 0.5),
          borderRadius: 10, cursor: 'grab' }}>
          <PlanetOrb kind={s.type} size={26} theme={theme} ring={s.type === 'approval'} />
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 12, fontWeight: 600, color: theme.text }}>{s.label}</div>
            <div style={{ fontFamily: theme.mono, fontSize: 9.5, color: theme.textMute, marginTop: 1 }}>{s.desc}</div>
          </div>
        </div>
      ))}
      <div style={{ flex: 1 }} />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 7, paddingTop: 11,
        borderTop: `1px solid ${theme.line}` }}>
        <Btn kind="secondary">Validate orbit</Btn>
        <Btn kind="primary">▶ Launch run</Btn>
      </div>
    </aside>
  );
}

Object.assign(window, { CookerMark, CosmicSidebar, EditorHeader, Palette, STAGE_DEFS });
