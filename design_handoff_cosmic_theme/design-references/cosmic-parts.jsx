/* Cosmic Cooker — shared parts (planets, trajectories, chrome) */
const { useMemo } = React;

// ----------------------------------------------------------------- Starfield
function Starfield({ theme, seed = 1, density = 90, nebula = true }) {
  const stars = useMemo(() => {
    let s = seed * 9301 + 49297;
    const rnd = () => { s = (s * 9301 + 49297) % 233280; return s / 233280; };
    return Array.from({ length: density }, () => ({
      x: rnd() * 100, y: rnd() * 100,
      r: rnd() * 1.4 + 0.3,
      o: rnd() * 0.7 + 0.2,
      d: rnd() * 4 + 2,
      delay: rnd() * 4,
    }));
  }, [seed, density]);
  return (
    <div style={{ position: 'absolute', inset: 0, overflow: 'hidden', pointerEvents: 'none' }}>
      {nebula && (
        <React.Fragment>
          <div style={{ position: 'absolute', width: '60%', height: '60%', left: '-10%', top: '-15%',
            background: `radial-gradient(circle, ${hexA(theme.violetGlow, theme.mode === 'dark' ? 0.28 : 0.14)} 0%, transparent 65%)`,
            filter: 'blur(20px)' }} />
          <div style={{ position: 'absolute', width: '55%', height: '55%', right: '-8%', bottom: '-12%',
            background: `radial-gradient(circle, ${hexA(theme.cyan, theme.mode === 'dark' ? 0.18 : 0.10)} 0%, transparent 65%)`,
            filter: 'blur(24px)' }} />
          <div style={{ position: 'absolute', width: '45%', height: '40%', right: '18%', top: '-6%',
            background: `radial-gradient(circle, ${hexA(theme.ember, theme.mode === 'dark' ? 0.10 : 0.08)} 0%, transparent 60%)`,
            filter: 'blur(26px)' }} />
        </React.Fragment>
      )}
      {stars.map((st, i) => (
        <span key={i} style={{
          position: 'absolute', left: st.x + '%', top: st.y + '%',
          width: st.r * 2, height: st.r * 2, borderRadius: 999,
          background: theme.mode === 'dark' ? '#fff' : theme.violet,
          opacity: st.o * theme.stars,
          boxShadow: theme.mode === 'dark' ? `0 0 ${st.r * 3}px ${hexA('#cdd6ff', 0.8)}` : 'none',
          animation: `ccTwinkle ${st.d}s ease-in-out ${st.delay}s infinite`,
        }} />
      ))}
    </div>
  );
}

// ----------------------------------------------------------------- PlanetOrb
function PlanetOrb({ kind, size = 36, status, theme, ring = false }) {
  const k = PLANET_KIND[kind] || PLANET_KIND.custom;
  const live = status === 'running' || status === 'building';
  const haloColor = live ? theme.ember : k.glow;
  return (
    <div style={{ position: 'relative', width: size, height: size, flexShrink: 0 }}>
      {/* halo */}
      <div style={{ position: 'absolute', inset: -size * 0.34, borderRadius: 999,
        background: `radial-gradient(circle, ${hexA(haloColor, live ? 0.55 : 0.34)} 0%, transparent 70%)`,
        animation: live ? 'ccHalo 1.8s ease-in-out infinite' : 'none' }} />
      {/* orbit ring for live / explicit ring */}
      {(live || ring) && (
        <div style={{ position: 'absolute', inset: -size * 0.22, borderRadius: 999,
          border: `1px solid ${hexA(live ? theme.ember : k.glow, 0.5)}`,
          transform: ring && !live ? 'rotateX(72deg)' : 'none',
          animation: live ? 'ccSpin 6s linear infinite' : 'none' }}>
          {live && <span style={{ position: 'absolute', top: -2, left: '50%', width: 4, height: 4, borderRadius: 999,
            background: theme.ember, boxShadow: `0 0 6px ${theme.ember}` }} />}
        </div>
      )}
      {/* planet body */}
      <div style={{ position: 'absolute', inset: 0, borderRadius: 999,
        background: `radial-gradient(circle at 33% 28%, ${k.from} 0%, ${k.to} 72%, ${hexA(k.to, 0.7)} 100%)`,
        boxShadow: `inset -3px -4px 8px ${hexA('#000', 0.35)}, inset 2px 2px 5px ${hexA('#fff', 0.4)}, 0 0 ${size * 0.4}px ${hexA(haloColor, 0.5)}`,
        display: 'grid', placeItems: 'center',
        color: hexA('#1a1030', 0.7), fontSize: size * 0.42, fontWeight: 700 }}>
        <span style={{ opacity: 0.55, textShadow: `0 1px 1px ${hexA('#fff', 0.3)}` }}>{k.ch}</span>
      </div>
      {/* saturn-style ring overlay */}
      {ring && (
        <div style={{ position: 'absolute', left: -size * 0.34, top: '50%', width: size * 1.68, height: size * 0.5,
          marginTop: -size * 0.25, borderRadius: '50%',
          border: `${Math.max(2, size * 0.07)}px solid ${hexA(k.glow, 0.7)}`,
          borderTopColor: hexA(k.glow, 0.25), borderBottomColor: hexA(k.glow, 0.9),
          transform: 'rotate(-18deg)', pointerEvents: 'none' }} />
      )}
    </div>
  );
}

// ----------------------------------------------------------------- PlanetNode
function PlanetNode({ node, theme, selected, style }) {
  const k = PLANET_KIND[node.kind] || PLANET_KIND.custom;
  const live = node.status === 'running' || node.status === 'building';
  const sc = statusColor(theme, node.status);
  const dim = node.status === 'pending' || node.status === 'queued';
  const ring = node.kind === 'approval';
  return (
    <div style={{
      position: 'absolute', width: 214, ...style,
      background: theme.surface, backdropFilter: 'blur(10px)', WebkitBackdropFilter: 'blur(10px)',
      border: `1px solid ${selected ? theme.violet : live ? hexA(theme.ember, 0.5) : theme.line}`,
      borderRadius: 14, padding: '11px 13px',
      opacity: dim ? 0.62 : 1,
      boxShadow: selected
        ? `0 0 0 3px ${hexA(theme.violet, 0.28)}, 0 14px 40px ${hexA('#000', theme.mode === 'dark' ? 0.5 : 0.14)}`
        : live
        ? `0 0 26px ${hexA(theme.ember, 0.3)}, 0 14px 38px ${hexA('#000', theme.mode === 'dark' ? 0.5 : 0.14)}`
        : `0 12px 30px ${hexA('#000', theme.mode === 'dark' ? 0.42 : 0.10)}`,
      overflow: 'hidden',
    }}>
      {/* atmosphere edge glow */}
      <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, width: 46,
        background: `radial-gradient(ellipse at left center, ${hexA(live ? theme.ember : k.glow, 0.30)} 0%, transparent 75%)`,
        pointerEvents: 'none' }} />
      {/* ports */}
      <Port side="left" color={theme.violetGlow} theme={theme} />
      <Port side="right" color={theme.violetGlow} theme={theme} />

      <div style={{ display: 'flex', alignItems: 'center', gap: 11, position: 'relative' }}>
        <PlanetOrb kind={node.kind} size={34} status={node.status} theme={theme} ring={ring} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontFamily: theme.sans, fontSize: 13, fontWeight: 600, color: theme.text,
            whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', letterSpacing: -0.1 }}>
            {node.label}
          </div>
          <div style={{ fontFamily: theme.mono, fontSize: 10, color: theme.textMute, marginTop: 3,
            whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
            {node.detail}
          </div>
        </div>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 7, marginTop: 9, position: 'relative' }}>
        <span style={{ width: 7, height: 7, borderRadius: 999, background: sc, flexShrink: 0,
          boxShadow: live ? `0 0 0 3px ${hexA(sc, 0.22)}` : 'none',
          animation: live ? 'ccPulse 1.6s ease-out infinite' : 'none' }} />
        <span style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 0.7, textTransform: 'uppercase',
          color: live ? theme.ember : theme.textMute }}>
          {node.duration || node.status}
        </span>
        {live && (
          <div style={{ flex: 1, height: 3, marginLeft: 4, borderRadius: 2, overflow: 'hidden',
            background: hexA(theme.ember, 0.18) }}>
            <div style={{ width: '55%', height: '100%', borderRadius: 2,
              background: `linear-gradient(90deg, transparent, ${theme.ember}, transparent)`,
              animation: 'ccShimmer 1.4s ease-in-out infinite' }} />
          </div>
        )}
        {node.env && !live && (
          <span style={{ marginLeft: 'auto', fontFamily: theme.mono, fontSize: 9.5, color: theme.cool,
            padding: '1px 6px', borderRadius: 999, border: `1px solid ${hexA(theme.cool, 0.3)}`,
            background: hexA(theme.cool, 0.1) }}>{node.env}</span>
        )}
      </div>
    </div>
  );
}

function Port({ side, color, theme }) {
  return (
    <span style={{ position: 'absolute', top: '50%', [side]: -5, transform: 'translateY(-50%)',
      width: 9, height: 9, borderRadius: 999, background: color,
      border: `1.5px solid ${theme.mode === 'dark' ? theme.bg : '#fff'}`,
      boxShadow: `0 0 8px ${hexA(color, 0.8)}` }} />
  );
}

// ----------------------------------------------------------------- Trajectory
// Renders one SVG path (planet → planet) with glow + optional travelling comet.
function trajectoryPath(a, b) {
  // a, b are {x,y} port points
  const dx = Math.max(60, Math.abs(b.x - a.x) * 0.5);
  return `M ${a.x} ${a.y} C ${a.x + dx} ${a.y}, ${b.x - dx} ${b.y}, ${b.x} ${b.y}`;
}

function Trajectory({ a, b, theme, state = 'idle', id }) {
  const path = trajectoryPath(a, b);
  const color = state === 'done' ? theme.good : state === 'active' ? theme.ember : theme.line;
  const grad = `traj-${id}`;
  const active = state === 'active';
  const done = state === 'done';
  return (
    <g>
      <defs>
        <linearGradient id={grad} x1="0" y1="0" x2="1" y2="0">
          <stop offset="0%" stopColor={done ? theme.good : active ? theme.violetGlow : theme.lineStrong} />
          <stop offset="100%" stopColor={done ? theme.good : active ? theme.ember : theme.line} />
        </linearGradient>
      </defs>
      {/* glow underlay */}
      {(active || done) && (
        <path d={path} fill="none" stroke={active ? theme.ember : theme.good}
          strokeWidth={active ? 7 : 5} strokeLinecap="round" opacity={0.22}
          style={{ filter: 'blur(4px)' }} />
      )}
      <path d={path} fill="none" stroke={`url(#${grad})`}
        strokeWidth={active ? 2.4 : 1.6} strokeLinecap="round"
        strokeDasharray={state === 'idle' ? '1 7' : active ? '8 6' : 'none'}
        style={{ animation: active ? 'ccDash 0.9s linear infinite' : 'none' }} />
      {active && (
        <circle r="3.6" fill="#fff" style={{ filter: `drop-shadow(0 0 5px ${theme.ember})` }}>
          <animateMotion dur="1.8s" repeatCount="indefinite" path={path} />
        </circle>
      )}
    </g>
  );
}

// ----------------------------------------------------------------- Graph
function CosmicGraph({ theme, nodes, edges, selectedId, width, height, pan = { x: 0, y: 0 } }) {
  const byId = Object.fromEntries(nodes.map((n) => [n.id, n]));
  const NW = 214, NH = 78;
  const port = (n, side) => ({
    x: pan.x + n.x + (side === 'right' ? NW : 0),
    y: pan.y + n.y + NH / 2,
  });
  return (
    <div style={{ position: 'absolute', inset: 0, overflow: 'hidden' }}>
      <svg width={width} height={height} style={{ position: 'absolute', inset: 0, pointerEvents: 'none' }}>
        {edges.map((e, i) => (
          <Trajectory key={i} id={i} theme={theme}
            a={port(byId[e.from], 'right')} b={port(byId[e.to], 'left')} state={e.state} />
        ))}
      </svg>
      {nodes.map((n) => (
        <PlanetNode key={n.id} node={n} theme={theme} selected={n.id === selectedId}
          style={{ left: pan.x + n.x, top: pan.y + n.y }} />
      ))}
    </div>
  );
}

Object.assign(window, { Starfield, PlanetOrb, PlanetNode, Trajectory, CosmicGraph, trajectoryPath });
