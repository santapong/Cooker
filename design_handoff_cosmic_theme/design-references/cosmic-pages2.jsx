/* Cosmic Cooker — pages (2): Run Mission Control, Registry, Environments */

// ----------------------------------------------------------------- Run: Mission Control
const RUN_STAGES = [
  { name: 'clone repo', kind: 'source', status: 'done', dur: '3.1s' },
  { name: 'build image', kind: 'build', status: 'done', dur: '47s' },
  { name: 'sast scan', kind: 'custom', status: 'done', dur: '12s' },
  { name: 'unit tests', kind: 'test', status: 'running', dur: '00:38' },
  { name: 'push image', kind: 'push', status: 'pending', dur: '—' },
  { name: 'deploy', kind: 'deploy', status: 'pending', dur: '—' },
];
const LOG_LINES = [
  { ts: '12:41:02', lvl: 'info', msg: '→ go test ./... -race -count=1' },
  { ts: '12:41:03', lvl: 'ok', msg: 'ok   pkg/dagrunner        0.42s' },
  { ts: '12:41:04', lvl: 'ok', msg: 'ok   pkg/ociutil          0.18s' },
  { ts: '12:41:06', lvl: 'ok', msg: 'ok   internal/oci         1.18s' },
  { ts: '12:41:08', lvl: 'warn', msg: 'data race avoided: retry budget low (2 left)' },
  { ts: '12:41:09', lvl: 'ok', msg: 'ok   internal/builder     2.04s' },
  { ts: '12:41:12', lvl: 'ok', msg: 'ok   internal/pusher      0.91s' },
  { ts: '12:41:14', lvl: 'info', msg: 'RUN  TestExecutor_FanOut/bounded' },
  { ts: '12:41:15', lvl: 'ok', msg: 'ok   internal/jobqueue    1.33s' },
  { ts: '12:41:17', lvl: 'info', msg: 'RUN  internal/service · 14 packages remaining' },
];

function StepPlanet({ theme, status, kind }) {
  const done = status === 'done', live = status === 'running';
  const c = done ? theme.good : live ? theme.ember : theme.textMute;
  return (
    <div style={{ width: 30, height: 30, borderRadius: 999, display: 'grid', placeItems: 'center', position: 'relative',
      background: theme.mode === 'dark' ? hexA('#000', 0.3) : '#fff', border: `2px solid ${c}`,
      boxShadow: live ? `0 0 12px ${hexA(theme.ember, 0.7)}` : done ? `0 0 8px ${hexA(theme.good, 0.4)}` : 'none' }}>
      {done && <span style={{ color: c, fontSize: 13 }}>✓</span>}
      {live && <span style={{ width: 9, height: 9, borderRadius: 999, background: c, animation: 'ccPulse 1.4s ease-out infinite' }} />}
      {status === 'pending' && <span style={{ width: 5, height: 5, borderRadius: 999, background: theme.textMute }} />}
    </div>
  );
}

function RunMissionControl({ theme }) {
  const lvlColor = { ok: theme.good, info: theme.textSoft, warn: theme.warn, error: theme.bad };
  const envs = [
    { name: 'dev', status: 'deployed', c: theme.good },
    { name: 'staging', status: 'deploying', c: theme.ember },
    { name: 'production', status: 'awaiting', c: theme.warn },
  ];
  return (
    <AppShell theme={theme} active="Pipelines" scroll={false}
      crumbs={[{ label: 'Cooker' }, { label: 'Pipelines' }, { label: 'checkout-api', mono: true }, { label: 'run #4b2f1a', mono: true }]}>
      <div style={{ height: '100%', display: 'grid', gridTemplateColumns: '296px 1fr 312px' }}>
        {/* step rail */}
        <aside style={{ borderRight: `1px solid ${theme.line}`, background: theme.panelGlass,
          backdropFilter: 'blur(12px)', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div style={{ padding: '18px 20px 14px' }}>
            <div style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 1.4, textTransform: 'uppercase', color: theme.textMute }}>Flight log · run #4b2f1a</div>
            <div style={{ fontFamily: theme.display, fontSize: 21, fontWeight: 600, color: theme.text, marginTop: 4 }}>checkout-api</div>
            <div style={{ display: 'flex', gap: 6, marginTop: 9 }}>
              <CPill theme={theme} color={theme.ember}>● running</CPill>
              <CPill theme={theme} color={theme.textMute}>12:41:02</CPill>
            </div>
          </div>
          <div style={{ position: 'relative', flex: 1, padding: '4px 0', overflow: 'auto' }}>
            <span style={{ position: 'absolute', left: 35, top: 20, bottom: 20, width: 1,
              background: `linear-gradient(${theme.good}, ${theme.ember} 55%, ${theme.line} 60%)` }} />
            {RUN_STAGES.map((s, i) => {
              const live = s.status === 'running', sel = live;
              return (
                <div key={i} style={{ display: 'grid', gridTemplateColumns: '46px 1fr auto', alignItems: 'center', gap: 8,
                  padding: '11px 18px 11px 14px', cursor: 'pointer',
                  background: sel ? hexA(theme.ember, 0.08) : 'transparent',
                  borderLeft: `3px solid ${sel ? theme.ember : 'transparent'}` }}>
                  <StepPlanet theme={theme} status={s.status} kind={s.kind} />
                  <div style={{ minWidth: 0 }}>
                    <div style={{ fontSize: 12.5, fontWeight: 600, color: theme.text }}>{s.name}</div>
                    <div style={{ fontFamily: theme.mono, fontSize: 10, color: theme.textMute, marginTop: 2 }}>{s.kind}</div>
                  </div>
                  <span style={{ fontFamily: theme.mono, fontSize: 11, color: live ? theme.ember : theme.textMute }}>{s.dur}</span>
                </div>
              );
            })}
          </div>
          <div style={{ padding: '12px 18px', borderTop: `1px solid ${theme.line}` }}>
            <CBtn theme={theme} kind="danger" style={{ width: '100%' }}>✕ Abort run</CBtn>
          </div>
        </aside>

        {/* logs */}
        <section style={{ display: 'flex', flexDirection: 'column', minWidth: 0, position: 'relative', overflow: 'hidden',
          background: theme.mode === 'dark' ? '#05050f' : '#0c0a1e' }}>
          <Starfield theme={{ ...theme, mode: 'dark', stars: 0.7 }} seed={51} density={70} nebula={false} />
          <div style={{ position: 'relative', display: 'flex', alignItems: 'center', gap: 12, padding: '11px 18px',
            borderBottom: `1px solid ${hexA('#8a7ee6', 0.18)}`, background: hexA('#0c0a1e', 0.6), backdropFilter: 'blur(8px)' }}>
            <PlanetOrb kind="test" size={26} status="running" theme={theme} />
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: 13, fontWeight: 600, color: '#ECEBFF' }}>unit tests</div>
              <div style={{ fontFamily: theme.mono, fontSize: 10, color: '#8a8cc0' }}>test · 142 lines · go test ./... -race</div>
            </div>
            <div style={{ display: 'flex', padding: 2, borderRadius: 6, gap: 2, background: hexA('#fff', 0.06), border: `1px solid ${hexA('#8a7ee6', 0.2)}` }}>
              {['all', 'info', 'warn', 'error'].map((f, i) => (
                <span key={f} style={{ padding: '3px 9px', borderRadius: 4, fontFamily: theme.mono, fontSize: 10,
                  textTransform: 'uppercase', letterSpacing: 0.5, fontWeight: 600, cursor: 'pointer',
                  background: i === 0 ? hexA('#8B6DFF', 0.25) : 'transparent', color: i === 0 ? '#C0A8FF' : '#7a7ca8' }}>{f}</span>
              ))}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontFamily: theme.mono, fontSize: 10.5,
              color: theme.good, padding: '5px 10px', borderRadius: 6, border: `1px solid ${hexA(theme.good, 0.4)}`, background: hexA(theme.good, 0.1) }}>
              <span style={{ width: 6, height: 6, borderRadius: 999, background: theme.good, animation: 'ccPulse 1.6s ease-out infinite' }} />tail · live</div>
          </div>
          <div style={{ position: 'relative', flex: 1, overflow: 'auto', padding: '12px 0', fontFamily: theme.mono, fontSize: 12 }}>
            {LOG_LINES.map((l, i) => (
              <div key={i} style={{ display: 'grid', gridTemplateColumns: '78px 54px 1fr', gap: 12, padding: '2.5px 18px', lineHeight: 1.6 }}>
                <span style={{ color: '#5a5c8a' }}>{l.ts}</span>
                <span style={{ color: lvlColor[l.lvl], textTransform: 'uppercase', fontSize: 10, letterSpacing: 0.6, paddingTop: 1.5 }}>{l.lvl}</span>
                <span style={{ color: '#d6d8ff' }}>{l.msg}</span>
              </div>
            ))}
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 18px', color: theme.ember }}>
              <span style={{ width: 6, height: 6, borderRadius: 999, background: theme.ember, animation: 'ccPulse 1.2s infinite' }} />
              <span style={{ width: 7, height: 13, background: theme.ember, animation: 'ccBlink 1s step-end infinite' }} />
            </div>
          </div>
          <div style={{ position: 'relative', padding: '8px 18px', borderTop: `1px solid ${hexA('#8a7ee6', 0.18)}`,
            display: 'flex', gap: 12, fontFamily: theme.mono, fontSize: 10.5, color: '#7a7ca8', background: hexA('#0c0a1e', 0.6) }}>
            <span>142 lines</span><span>·</span><span style={{ color: theme.warn }}>1 warning</span><span>·</span><span style={{ color: theme.good }}>0 errors</span>
            <span style={{ flex: 1 }} /><span>running</span>
          </div>
        </section>

        {/* promotion path */}
        <aside style={{ borderLeft: `1px solid ${theme.line}`, background: theme.panelGlass,
          backdropFilter: 'blur(12px)', padding: '18px 18px', display: 'flex', flexDirection: 'column', gap: 16, overflow: 'auto' }}>
          <SLabel theme={theme}>Promotion trajectory</SLabel>
          <div style={{ position: 'relative', display: 'flex', flexDirection: 'column', gap: 12 }}>
            {envs.map((e, i) => (
              <div key={e.name} style={{ position: 'relative' }}>
                {i < envs.length - 1 && <span style={{ position: 'absolute', left: 19, top: 44, height: 24, width: 2,
                  background: `repeating-linear-gradient(${hexA(e.c, 0.6)} 0 3px, transparent 3px 7px)` }} />}
                <div style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '12px 13px', borderRadius: 12,
                  background: hexA(theme.surfaceSolid, 0.5), border: `1px solid ${hexA(e.c, 0.35)}` }}>
                  <PlanetOrb kind={i === 0 ? 'source' : i === 1 ? 'test' : 'deploy'} size={28} status={e.status === 'deploying' ? 'running' : undefined} theme={theme} />
                  <div style={{ flex: 1 }}>
                    <div style={{ fontFamily: theme.mono, fontSize: 12.5, fontWeight: 600, color: theme.text }}>{e.name}</div>
                    <div style={{ fontFamily: theme.mono, fontSize: 10, color: theme.textMute, marginTop: 2 }}>{i === 2 ? 'manual gate' : 'auto-promote'}</div>
                  </div>
                  <CPill theme={theme} color={e.c}>{e.status}</CPill>
                </div>
                {e.status === 'awaiting' && (
                  <div style={{ marginTop: 8 }}><CBtn theme={theme} kind="primary" style={{ width: '100%' }}>✓ Approve production</CBtn></div>
                )}
              </div>
            ))}
          </div>
          <SLabel theme={theme}>Live telemetry</SLabel>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 9 }}>
            {[['cpu', 62, theme.cyan], ['memory', 48, theme.violet], ['net i/o', 81, theme.ember]].map(([k, v, c]) => (
              <div key={k}>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontFamily: theme.mono, fontSize: 10.5, color: theme.textSoft, marginBottom: 4 }}>
                  <span style={{ textTransform: 'uppercase', letterSpacing: 0.6 }}>{k}</span><span style={{ color: c }}>{v}%</span></div>
                <div style={{ height: 5, borderRadius: 3, background: theme.line, overflow: 'hidden' }}>
                  <div style={{ width: v + '%', height: '100%', background: c, borderRadius: 3, boxShadow: `0 0 8px ${hexA(c, 0.6)}` }} /></div>
              </div>
            ))}
          </div>
        </aside>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- Registry
const REPOS = [
  { name: 'acme/checkout', tags: 24, pushed: '2m ago', size: '142 MB', sel: true },
  { name: 'acme/billing', tags: 18, pushed: '1h ago', size: '98 MB' },
  { name: 'acme/marketing', tags: 9, pushed: '3h ago', size: '24 MB' },
  { name: 'acme/search', tags: 31, pushed: '6h ago', size: '210 MB' },
  { name: 'acme/auth', tags: 12, pushed: '1d ago', size: '76 MB' },
  { name: 'acme/imgr', tags: 7, pushed: '2d ago', size: '54 MB' },
];
const TAGS = [
  { tag: '8f3c1a', digest: 'sha256:9b2f…e41', size: '142 MB', arch: 'amd64 · arm64', latest: true },
  { tag: 'v1.4.2', digest: 'sha256:7a0d…c19', size: '142 MB', arch: 'amd64 · arm64' },
  { tag: 'v1.4.1', digest: 'sha256:31bc…88a', size: '141 MB', arch: 'amd64' },
];

function RegistryPage({ theme }) {
  return (
    <AppShell theme={theme} active="Registry" crumbs={[{ label: 'Cooker' }, { label: 'Registry' }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        <CPageHeader theme={theme} eyebrow="OCI distribution-spec v1.1"
          title="Registry"
          subtitle="Every image is an artifact in orbit. Inspect tags, digests, and OCI referrers — signatures, SBOMs, and build provenance circling each manifest."
          actions={<React.Fragment><CBtn theme={theme} kind="ghost">⚙ Configure</CBtn><CBtn theme={theme} kind="primary">⬡ Pull image</CBtn></React.Fragment>} />
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 372px', gap: 20 }}>
          {/* repos */}
          <GlassCard theme={theme} pad={0} style={{ overflow: 'hidden', alignSelf: 'start' }}>
            <div style={{ padding: '14px 18px', borderBottom: `1px solid ${theme.line}`, display: 'flex', alignItems: 'center', gap: 11 }}>
              <span style={{ fontFamily: theme.display, fontSize: 16, fontWeight: 600, color: theme.text }}>Repositories</span>
              <CPill theme={theme} color={theme.textMute}>6</CPill>
              <div style={{ flex: 1 }} />
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: 240, padding: '5px 10px', borderRadius: 8,
                background: hexA(theme.surfaceSolid, 0.5), border: `1px solid ${theme.line}`, color: theme.textMute }}>
                <span style={{ fontSize: 12 }}>⌕</span><span style={{ fontFamily: theme.mono, fontSize: 11.5, flex: 1 }}>Filter repositories…</span></div>
            </div>
            {REPOS.map((r, i) => (
              <div key={r.name} style={{ display: 'grid', gridTemplateColumns: '1fr 90px 110px 90px', alignItems: 'center',
                borderTop: `1px solid ${theme.line}`, cursor: 'pointer', minHeight: 54,
                background: r.sel ? hexA(theme.violet, 0.1) : i % 2 ? hexA(theme.violet, 0.03) : 'transparent',
                borderLeft: `3px solid ${r.sel ? theme.violet : 'transparent'}` }}>
                <div style={{ padding: '12px 16px', display: 'flex', alignItems: 'center', gap: 11 }}>
                  <PlanetOrb kind="push" size={24} theme={theme} ring />
                  <span style={{ fontFamily: theme.mono, fontSize: 12.5, color: theme.text, fontWeight: 600 }}>{r.name}</span>
                </div>
                <div style={{ padding: '12px 8px', fontFamily: theme.mono, fontSize: 11, color: theme.textSoft }}>{r.tags} tags</div>
                <div style={{ padding: '12px 8px', fontFamily: theme.mono, fontSize: 11, color: theme.textMute }}>{r.pushed}</div>
                <div style={{ padding: '12px 16px', fontFamily: theme.mono, fontSize: 11, color: theme.textSoft, textAlign: 'right' }}>{r.size}</div>
              </div>
            ))}
          </GlassCard>
          {/* tag inspector */}
          <GlassCard theme={theme} pad={0} style={{ overflow: 'hidden', alignSelf: 'start' }}>
            <div style={{ padding: '16px 18px', borderBottom: `1px solid ${theme.line}`, display: 'flex', alignItems: 'center', gap: 12 }}>
              <PlanetOrb kind="push" size={36} theme={theme} ring />
              <div><div style={{ fontFamily: theme.mono, fontSize: 13.5, fontWeight: 600, color: theme.text }}>acme/checkout</div>
                <div style={{ fontFamily: theme.mono, fontSize: 10, color: theme.textMute, marginTop: 3 }}>24 tags · image index</div></div>
            </div>
            <div style={{ padding: '14px 18px' }}>
              <SLabel theme={theme} style={{ marginBottom: 12 }}>Tags in orbit</SLabel>
              {TAGS.map((tg) => (
                <div key={tg.tag} style={{ padding: '11px 12px', borderRadius: 10, marginBottom: 9,
                  background: hexA(theme.surfaceSolid, 0.5), border: `1px solid ${theme.line}` }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ fontFamily: theme.mono, fontSize: 12.5, color: theme.text, fontWeight: 600 }}>{tg.tag}</span>
                    {tg.latest && <CPill theme={theme} color={theme.good}>latest</CPill>}
                    <span style={{ flex: 1 }} />
                    <span style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute }}>{tg.size}</span>
                  </div>
                  <div style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textSoft, marginTop: 6 }}>{tg.digest}</div>
                  <div style={{ fontFamily: theme.mono, fontSize: 10, color: theme.cool, marginTop: 4 }}>{tg.arch}</div>
                </div>
              ))}
              <SLabel theme={theme} style={{ margin: '16px 0 11px' }}>Referrers</SLabel>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {[['✓ cosign sig', theme.good], ['SBOM · spdx', theme.cyan], ['provenance', theme.violet]].map(([r, c]) => (
                  <span key={r} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontFamily: theme.mono, fontSize: 10.5,
                    padding: '6px 11px', borderRadius: 999, color: c, background: hexA(c, 0.12), border: `1px solid ${hexA(c, 0.3)}` }}>{r}</span>
                ))}
              </div>
            </div>
          </GlassCard>
        </div>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- Environments
function EnvironmentsPage({ theme }) {
  const envs = [
    { name: 'dev', kind: 'source', ns: 'cooker-dev', strat: 'auto', stratC: theme.cool, vars: 14, secrets: 6, deploy: 'just now', status: 'deployed', c: theme.good },
    { name: 'staging', kind: 'test', ns: 'cooker-staging', strat: 'auto', stratC: theme.cyan, vars: 16, secrets: 8, deploy: '8m ago', status: 'deploying', c: theme.ember },
    { name: 'production', kind: 'deploy', ns: 'cooker-prod', strat: 'manual gate', stratC: theme.warn, vars: 18, secrets: 11, deploy: '2h ago', status: 'awaiting', c: theme.warn },
  ];
  return (
    <AppShell theme={theme} active="Environments" crumbs={[{ label: 'Cooker' }, { label: 'Environments' }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        <CPageHeader theme={theme} eyebrow="promotion path"
          title="Environments"
          subtitle="Three worlds along one trajectory. Deploys land on the inner world first, then promote outward — auto or through a manual gate — until they reach production."
          actions={<CBtn theme={theme} kind="primary">＋ Add environment</CBtn>} />
        {/* trajectory of env planets */}
        <div style={{ position: 'relative', display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 18, marginBottom: 24 }}>
          {envs.map((e, i) => (
            <div key={e.name} style={{ position: 'relative' }}>
              {i < envs.length - 1 && (
                <svg width="40" height="20" viewBox="0 0 40 20" style={{ position: 'absolute', right: -29, top: 60, zIndex: 2 }}>
                  <path d="M2 10 H30" stroke={hexA(e.c, 0.6)} strokeWidth="1.6" strokeDasharray="3 3" />
                  <path d="M26 5 L33 10 L26 15" fill="none" stroke={e.c} strokeWidth="1.6" />
                </svg>
              )}
              <GlassCard theme={theme} pad={18} style={{ position: 'relative', overflow: 'hidden' }}>
                <div style={{ position: 'absolute', right: -24, top: -24, width: 100, height: 100, borderRadius: 999,
                  background: `radial-gradient(circle, ${hexA(e.c, 0.18)}, transparent 70%)` }} />
                <div style={{ display: 'flex', alignItems: 'center', gap: 13, position: 'relative' }}>
                  <PlanetOrb kind={e.kind} size={42} status={e.status === 'deploying' ? 'running' : undefined} theme={theme} ring={i === 2} />
                  <div style={{ flex: 1 }}>
                    <div style={{ fontFamily: theme.display, fontSize: 18, fontWeight: 600, color: theme.text }}>{e.name}</div>
                    <div style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute, marginTop: 2 }}>{e.ns}</div>
                  </div>
                  <CPill theme={theme} color={e.c}>{e.status}</CPill>
                </div>
                <div style={{ display: 'flex', gap: 8, marginTop: 14, position: 'relative' }}>
                  <CPill theme={theme} color={e.stratC}>{e.strat}</CPill>
                  <CPill theme={theme} color={theme.textMute}>{e.vars} vars</CPill>
                  <CPill theme={theme} color={theme.violet}>{e.secrets} secrets</CPill>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                  marginTop: 14, paddingTop: 12, borderTop: `1px solid ${theme.line}`, position: 'relative' }}>
                  <span style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute }}>deployed {e.deploy}</span>
                  <span style={{ fontFamily: theme.mono, fontSize: 11, color: theme.violet }}>manage →</span>
                </div>
              </GlassCard>
            </div>
          ))}
        </div>
        {/* secrets panel for production */}
        <GlassCard theme={theme} pad={0} style={{ overflow: 'hidden' }}>
          <div style={{ padding: '14px 18px', borderBottom: `1px solid ${theme.line}`, display: 'flex', alignItems: 'center', gap: 11 }}>
            <PlanetOrb kind="deploy" size={26} theme={theme} ring />
            <span style={{ fontFamily: theme.display, fontSize: 16, fontWeight: 600, color: theme.text }}>production · secrets</span>
            <CPill theme={theme} color={theme.violet}>vault backend</CPill>
            <div style={{ flex: 1 }} />
            <CBtn theme={theme} kind="secondary" style={{ padding: '6px 12px' }}>＋ Add secret</CBtn>
          </div>
          {[['DATABASE_URL', 'postgres://••••••@db.prod', 'rotated 3d ago'],
            ['STRIPE_SECRET_KEY', 'sk_live_••••••••••••', 'rotated 12d ago'],
            ['COOKER_SECRET_KEY', 'base64:••••••••••••••', 'rotated 1mo ago'],
            ['OIDC_CLIENT_SECRET', '••••••••••••••••••', 'rotated 5d ago']].map((s, i) => (
            <div key={s[0]} style={{ display: 'grid', gridTemplateColumns: '240px 1fr 140px 60px', alignItems: 'center',
              borderTop: `1px solid ${theme.line}`, background: i % 2 ? hexA(theme.violet, 0.03) : 'transparent', minHeight: 46 }}>
              <div style={{ padding: '10px 16px', fontFamily: theme.mono, fontSize: 12, color: theme.text, fontWeight: 600 }}>{s[0]}</div>
              <div style={{ padding: '10px 8px', fontFamily: theme.mono, fontSize: 11.5, color: theme.textMute, letterSpacing: 0.5 }}>{s[1]}</div>
              <div style={{ padding: '10px 8px', fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute }}>{s[2]}</div>
              <div style={{ padding: '10px 16px', textAlign: 'right', color: theme.textMute, fontSize: 13 }}>⋯</div>
            </div>
          ))}
        </GlassCard>
      </div>
    </AppShell>
  );
}

Object.assign(window, { RunMissionControl, RegistryPage, EnvironmentsPage });
