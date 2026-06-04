/* Cosmic Cooker — pages (1): Pipelines list + Apps */

// mini constellation preview for a pipeline card
function ConstellationThumb({ theme, kinds, live }) {
  const pts = [[14, 30], [40, 16], [40, 46], [66, 30], [92, 18], [92, 44]];
  const links = [[0, 1], [0, 2], [1, 3], [2, 3], [3, 4], [3, 5]];
  return (
    <svg width="110" height="60" viewBox="0 0 110 60">
      {links.map(([a, b], i) => (
        <line key={i} x1={pts[a][0]} y1={pts[a][1]} x2={pts[b][0]} y2={pts[b][1]}
          stroke={hexA(theme.violet, 0.35)} strokeWidth="1" strokeDasharray="2 3" />
      ))}
      {pts.map(([x, y], i) => {
        const kd = PLANET_KIND[kinds[i % kinds.length]] || PLANET_KIND.custom;
        const isLive = live && i === 3;
        return <circle key={i} cx={x} cy={y} r={isLive ? 5 : 3.4}
          fill={isLive ? theme.ember : kd.glow}
          style={{ filter: `drop-shadow(0 0 ${isLive ? 5 : 3}px ${isLive ? theme.ember : kd.glow})` }} />;
      })}
    </svg>
  );
}

const PIPELINES = [
  { name: 'checkout-api', kinds: ['source', 'build', 'test', 'push', 'deploy', 'custom'], steps: 6, status: 'running', statusColor: 'ember', desc: 'Build → test → push → rollout to k8s with prod gate.', updated: '2m ago', live: true },
  { name: 'billing-worker', kinds: ['source', 'build', 'test', 'push'], steps: 4, status: 'deployed', statusColor: 'good', desc: 'Redis-backed worker. Auto-promote dev → staging.', updated: '1h ago' },
  { name: 'marketing-site', kinds: ['source', 'build', 'push', 'deploy'], steps: 4, status: 'queued', statusColor: 'warn', desc: 'Static Vite build, pushed to CDN edge.', updated: '3h ago' },
  { name: 'search-indexer', kinds: ['source', 'build', 'custom', 'deploy'], steps: 5, status: 'deployed', statusColor: 'good', desc: 'Nightly reindex. Cron-triggered, fan-out 8.', updated: '6h ago' },
  { name: 'auth-gateway', kinds: ['source', 'build', 'test', 'approval', 'deploy'], steps: 7, status: 'failed', statusColor: 'bad', desc: 'OIDC edge. Manual approval before production.', updated: '1d ago' },
  { name: 'image-resizer', kinds: ['source', 'build', 'push'], steps: 3, status: 'deployed', statusColor: 'good', desc: 'Buildah rootless. Fly.io machines deploy.', updated: '2d ago' },
];

function PipelinesListPage({ theme }) {
  const sc = { ember: theme.ember, good: theme.good, warn: theme.warn, bad: theme.bad };
  return (
    <AppShell theme={theme} active="Pipelines" crumbs={[{ label: 'Cooker' }, { label: 'Pipelines' }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        <CPageHeader theme={theme} eyebrow="6 pipelines · 1 live"
          title="Pipelines"
          subtitle="Every pipeline is a constellation — drag planets onto the star map, wire their trajectories, and watch each world light up as it runs."
          actions={<React.Fragment>
            <CBtn theme={theme} kind="secondary">✺ Templates</CBtn>
            <CBtn theme={theme} kind="primary">＋ New pipeline</CBtn>
          </React.Fragment>} />
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(340px, 1fr))', gap: 18 }}>
          {PIPELINES.map((p) => (
            <GlassCard key={p.name} theme={theme} pad={18} style={{ position: 'relative', overflow: 'hidden', cursor: 'pointer' }}>
              <div style={{ position: 'absolute', right: -30, top: -30, width: 120, height: 120, borderRadius: 999,
                background: `radial-gradient(circle, ${hexA(p.live ? theme.ember : theme.violetGlow, 0.16)}, transparent 70%)` }} />
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', position: 'relative' }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontFamily: theme.display, fontSize: 19, fontWeight: 600, color: theme.text,
                    letterSpacing: -0.3 }}>{p.name}</div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 7, marginTop: 7 }}>
                    <StatusDot theme={theme} color={sc[p.statusColor]} pulse={p.live} />
                    <span style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 0.6, textTransform: 'uppercase',
                      color: sc[p.statusColor] }}>{p.status}</span>
                    <span style={{ color: theme.textFaint }}>·</span>
                    <CPill theme={theme} color={theme.violet}>{p.steps} planets</CPill>
                  </div>
                </div>
                <ConstellationThumb theme={theme} kinds={p.kinds} live={p.live} />
              </div>
              <p style={{ fontSize: 12.5, color: theme.textSoft, margin: '14px 0 14px', lineHeight: 1.55 }}>{p.desc}</p>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                borderTop: `1px solid ${theme.line}`, paddingTop: 11 }}>
                <span style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute }}>updated {p.updated}</span>
                <span style={{ fontFamily: theme.mono, fontSize: 11, color: theme.violet }}>open map →</span>
              </div>
            </GlassCard>
          ))}
        </div>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- Apps
const APPS = [
  { name: 'checkout-api', team: 'payments', env: 'prod', envC: 'violet', image: 'ghcr.io/acme/checkout:8f3c1a', health: 99, runs: 142, deploy: '2m ago', live: true, owner: 'CK' },
  { name: 'billing-worker', team: 'payments', env: 'prod', envC: 'violet', image: 'ghcr.io/acme/billing:4a1b', health: 98, runs: 88, deploy: '1h ago', owner: 'AS' },
  { name: 'marketing-site', team: 'growth', env: 'staging', envC: 'cyan', image: 'ghcr.io/acme/mktg:91fd', health: 100, runs: 31, deploy: '3h ago', owner: 'JM' },
  { name: 'search-indexer', team: 'search', env: 'prod', envC: 'violet', image: 'ghcr.io/acme/search:77ce', health: 96, runs: 60, deploy: '6h ago', owner: 'RK' },
  { name: 'auth-gateway', team: 'platform', env: 'staging', envC: 'cyan', image: 'ghcr.io/acme/auth:2db0', health: 82, runs: 25, deploy: '1d ago', owner: 'TP' },
  { name: 'image-resizer', team: 'media', env: 'dev', envC: 'cool', image: 'ghcr.io/acme/imgr:b3a9', health: 100, runs: 12, deploy: '2d ago', owner: 'LM' },
];

function AppsListPage({ theme }) {
  const envColor = { violet: theme.violet, cyan: theme.cyan, cool: theme.cool };
  const stats = [
    { label: 'Apps in orbit', value: '6', sub: 'all healthy', color: theme.good },
    { label: 'Runs / 24h', value: '48', sub: '+12 vs yesterday', color: theme.cyan },
    { label: 'Mean build', value: '1m 12s', sub: 'kaniko · cached', color: theme.violet },
    { label: 'Open incidents', value: '0', sub: 'all clear', color: theme.good },
  ];
  const activity = [
    { t: '2m', who: 'checkout-api', what: 'deploy started → prod', c: theme.ember },
    { t: '14m', who: 'billing-worker', what: 'promoted to prod', c: theme.good },
    { t: '38m', who: 'auth-gateway', what: 'tests failed on staging', c: theme.bad },
    { t: '1h', who: 'marketing-site', what: 'image pushed · 91fd', c: theme.cyan },
    { t: '2h', who: 'search-indexer', what: 'reindex completed', c: theme.good },
  ];
  const headGrid = { display: 'grid', gridTemplateColumns: 'minmax(170px,1.4fr) 220px 180px 70px 120px 70px', alignItems: 'center' };
  return (
    <AppShell theme={theme} active="Apps" crumbs={[{ label: 'Cooker' }, { label: 'Apps' }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        <CPageHeader theme={theme} eyebrow="Tuesday, 3 Jun 2026"
          title={<React.Fragment>Good evening.<br /><span style={{ color: theme.textMute }}>6 apps in orbit, </span><span style={{ color: theme.good }}>all healthy.</span></React.Fragment>}
          actions={<React.Fragment>
            <CBtn theme={theme} kind="ghost">⚙ Filters</CBtn>
            <CBtn theme={theme} kind="secondary">⬡ Import Helm</CBtn>
            <CBtn theme={theme} kind="primary">＋ New app</CBtn>
          </React.Fragment>} />
        {/* stats */}
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 14, marginBottom: 24 }}>
          {stats.map((s) => (
            <GlassCard key={s.label} theme={theme} pad={17} accent={s.color}>
              <div style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 1.2, textTransform: 'uppercase',
                color: theme.textMute }}>{s.label}</div>
              <div style={{ fontFamily: theme.display, fontSize: 30, fontWeight: 600, color: theme.text,
                marginTop: 7, letterSpacing: -0.6 }}>{s.value}</div>
              <div style={{ fontSize: 11.5, color: s.color, marginTop: 4 }}>{s.sub}</div>
            </GlassCard>
          ))}
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 320px', gap: 20 }}>
          {/* services table */}
          <GlassCard theme={theme} pad={0} style={{ overflow: 'hidden' }}>
            <div style={{ padding: '14px 18px', borderBottom: `1px solid ${theme.line}`,
              display: 'flex', alignItems: 'center', gap: 11 }}>
              <span style={{ fontFamily: theme.display, fontSize: 16, fontWeight: 600, color: theme.text }}>Mission roster</span>
              <CPill theme={theme} color={theme.textMute}>6</CPill>
              <div style={{ flex: 1 }} />
              <div style={{ display: 'flex', padding: 2, borderRadius: 7, gap: 2,
                background: hexA(theme.surfaceSolid, 0.5), border: `1px solid ${theme.line}` }}>
                {['all', 'yours', 'broken'].map((f, i) => (
                  <span key={f} style={{ padding: '4px 10px', borderRadius: 5, fontFamily: theme.mono, fontSize: 10,
                    textTransform: 'uppercase', letterSpacing: 0.6, fontWeight: 600, cursor: 'pointer',
                    background: i === 0 ? hexA(theme.violet, 0.16) : 'transparent', color: i === 0 ? theme.violet : theme.textMute }}>{f}</span>
                ))}
              </div>
            </div>
            <div style={{ ...headGrid, padding: '0' }}>
              {['Service', 'Environment / image', 'Health', 'Runs', 'Last deploy', ''].map((h) => (
                <div key={h} style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 1, textTransform: 'uppercase',
                  color: theme.textMute, padding: '11px 14px' }}>{h}</div>
              ))}
            </div>
            {APPS.map((a, i) => (
              <div key={a.name} style={{ ...headGrid, borderTop: `1px solid ${theme.line}`,
                background: i % 2 ? hexA(theme.violet, 0.04) : 'transparent', cursor: 'pointer', minHeight: 52 }}>
                <div style={{ padding: '11px 14px', display: 'flex', alignItems: 'center', gap: 10 }}>
                  <StatusDot theme={theme} color={a.live ? theme.ember : theme.good} pulse={a.live} />
                  <div><div style={{ fontFamily: theme.mono, fontSize: 12.5, color: theme.text, fontWeight: 600 }}>{a.name}</div>
                    <div style={{ fontSize: 10.5, color: theme.textMute, marginTop: 2 }}>{a.team}</div></div>
                </div>
                <div style={{ padding: '11px 14px', display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                  <CPill theme={theme} color={envColor[a.envC]}>{a.env}</CPill>
                  <span style={{ fontFamily: theme.mono, fontSize: 11, color: theme.textSoft, whiteSpace: 'nowrap',
                    overflow: 'hidden', textOverflow: 'ellipsis' }}>{a.image}</span>
                </div>
                <div style={{ padding: '11px 14px', display: 'flex', alignItems: 'center', gap: 9 }}>
                  <HealthBar theme={theme} value={a.health} />
                  <span style={{ fontFamily: theme.mono, fontSize: 11, color: theme.textSoft }}>{a.health}%</span>
                </div>
                <div style={{ padding: '11px 14px', fontFamily: theme.mono, fontSize: 11.5, color: theme.textSoft }}>{a.runs}</div>
                <div style={{ padding: '11px 14px', fontSize: 11.5, color: a.live ? theme.ember : theme.textSoft }}>{a.deploy}</div>
                <div style={{ padding: '11px 14px', display: 'flex', alignItems: 'center', gap: 6, justifyContent: 'flex-end' }}>
                  <span style={{ width: 22, height: 22, borderRadius: 999, display: 'grid', placeItems: 'center',
                    background: `linear-gradient(135deg, ${theme.violet}, ${theme.cyan})`, color: '#fff',
                    fontFamily: theme.mono, fontWeight: 700, fontSize: 9 }}>{a.owner}</span>
                  <span style={{ color: theme.textMute, fontSize: 13 }}>→</span>
                </div>
              </div>
            ))}
          </GlassCard>
          {/* activity */}
          <GlassCard theme={theme} pad={0} style={{ overflow: 'hidden', alignSelf: 'start' }}>
            <div style={{ padding: '14px 18px', borderBottom: `1px solid ${theme.line}` }}>
              <span style={{ fontFamily: theme.display, fontSize: 16, fontWeight: 600, color: theme.text }}>Telemetry feed</span>
            </div>
            <div style={{ padding: '12px 8px 16px' }}>
              {activity.map((e, i) => (
                <div key={i} style={{ display: 'grid', gridTemplateColumns: '46px 16px 1fr', gap: 8, padding: '9px 14px' }}>
                  <span style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute, paddingTop: 3 }}>{e.t}</span>
                  <div style={{ display: 'flex', justifyContent: 'center', paddingTop: 6, position: 'relative' }}>
                    <StatusDot theme={theme} color={e.c} pulse={i === 0} />
                    {i < activity.length - 1 && <span style={{ position: 'absolute', top: 15, bottom: -10, width: 1, background: theme.line }} />}
                  </div>
                  <div><div style={{ fontFamily: theme.mono, fontSize: 11.5, color: theme.text, fontWeight: 600 }}>{e.who}</div>
                    <div style={{ fontSize: 11.5, color: theme.textSoft, marginTop: 1 }}>{e.what}</div></div>
                </div>
              ))}
            </div>
          </GlassCard>
        </div>
      </div>
    </AppShell>
  );
}

Object.assign(window, { PipelinesListPage, AppsListPage, ConstellationThumb });
