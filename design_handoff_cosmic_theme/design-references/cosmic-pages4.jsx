/* Cosmic Cooker — pages (4): Compose, Templates, Schedules, Notifications, Settings */

// ----------------------------------------------------------------- Compose editor
function ComposePage({ theme }) {
  const services = [
    { name: 'web', kind: 'deploy', img: 'acme/checkout:8f3c', ports: '8080:8080', deps: ['db', 'cache'] },
    { name: 'db', kind: 'source', img: 'postgres:16', ports: '5432', deps: [] },
    { name: 'cache', kind: 'custom', img: 'redis:7-alpine', ports: '6379', deps: [] },
  ];
  const yaml = [
    ['services', theme.cyan], ['  web', theme.text], ['    image: acme/checkout:8f3c', theme.textSoft],
    ['    ports: ["8080:8080"]', theme.textSoft], ['    depends_on: [db, cache]', theme.textSoft],
    ['  db', theme.text], ['    image: postgres:16', theme.textSoft],
    ['  cache', theme.text], ['    image: redis:7-alpine', theme.textSoft],
  ];
  return (
    <AppShell theme={theme} active="Apps" scroll={false} crumbs={[{ label: 'Cooker' }, { label: 'Docker' }, { label: 'Compose' }]}>
      <div style={{ height: '100%', display: 'grid', gridTemplateColumns: '1fr 420px' }}>
        {/* canvas of service planets */}
        <div style={{ position: 'relative', overflow: 'hidden',
          background: `radial-gradient(120% 100% at 30% 0%, ${theme.canvasTop}, ${theme.canvasBot})` }}>
          <Starfield theme={theme} seed={61} density={theme.mode === 'dark' ? 90 : 24} />
          <div style={{ position: 'absolute', inset: 0, opacity: 0.5,
            backgroundImage: `radial-gradient(${hexA(theme.violet, 0.1)} 1px, transparent 1px)`, backgroundSize: '34px 34px' }} />
          <div style={{ position: 'absolute', top: 16, left: 18, fontFamily: theme.mono, fontSize: 11, color: theme.textMute, letterSpacing: 0.5 }}>compose · 3 services · 2 links</div>
          <svg width="100%" height="100%" style={{ position: 'absolute', inset: 0, pointerEvents: 'none' }}>
            <path d="M250,150 C250,260 250,300 180,330" fill="none" stroke={hexA(theme.violet, 0.5)} strokeWidth="1.6" strokeDasharray="6 5" />
            <path d="M250,150 C250,260 250,300 360,330" fill="none" stroke={hexA(theme.violet, 0.5)} strokeWidth="1.6" strokeDasharray="6 5" />
          </svg>
          {[{ x: 250, y: 120, s: services[0] }, { x: 180, y: 330, s: services[1] }, { x: 360, y: 330, s: services[2] }].map((n) => (
            <div key={n.s.name} style={{ position: 'absolute', left: n.x - 95, top: n.y - 35, width: 190 }}>
              <GlassCard theme={theme} pad={12}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <PlanetOrb kind={n.s.kind} size={30} theme={theme} />
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontFamily: theme.mono, fontSize: 13, fontWeight: 600, color: theme.text }}>{n.s.name}</div>
                    <div style={{ fontFamily: theme.mono, fontSize: 9.5, color: theme.textMute, marginTop: 2, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{n.s.img}</div>
                  </div>
                </div>
                <div style={{ marginTop: 9, fontFamily: theme.mono, fontSize: 10, color: theme.cyan }}>↳ {n.s.ports}</div>
              </GlassCard>
            </div>
          ))}
        </div>
        {/* yaml panel */}
        <aside style={{ borderLeft: `1px solid ${theme.line}`, background: theme.panelGlass, backdropFilter: 'blur(12px)',
          display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div style={{ padding: '14px 18px', borderBottom: `1px solid ${theme.line}`, display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ fontFamily: theme.display, fontSize: 15, fontWeight: 600, color: theme.text }}>docker-compose.yml</span>
            <CPill theme={theme} color={theme.good}>valid</CPill>
            <div style={{ flex: 1 }} /><CBtn theme={theme} kind="primary" style={{ padding: '6px 12px' }}>▶ Up</CBtn>
          </div>
          <div style={{ flex: 1, overflow: 'auto', padding: '16px 18px', fontFamily: theme.mono, fontSize: 12.5, lineHeight: 1.8,
            background: theme.mode === 'dark' ? hexA('#000', 0.3) : hexA('#1a1730', 0.03) }}>
            {yaml.map((ln, i) => (
              <div key={i} style={{ display: 'flex', gap: 14 }}>
                <span style={{ color: theme.textFaint, width: 16, textAlign: 'right', flexShrink: 0 }}>{i + 1}</span>
                <span style={{ color: ln[1], whiteSpace: 'pre' }}>{ln[0]}</span>
              </div>
            ))}
          </div>
        </aside>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- Templates gallery
function TemplatesPage({ theme }) {
  const tpls = [
    { name: 'Go service · k8s', kinds: ['source', 'build', 'test', 'push', 'deploy'], desc: 'Build, race-test, push OCI, rollout to Kubernetes with a prod gate.', uses: 142, tag: 'popular', tagC: theme.ember },
    { name: 'Node static · CDN', kinds: ['source', 'build', 'push'], desc: 'Vite build pushed to an edge CDN bucket.', uses: 86, tag: 'web', tagC: theme.cyan },
    { name: 'Worker · Redis', kinds: ['source', 'build', 'test', 'deploy'], desc: 'Background worker with Redis, auto-promote dev→staging.', uses: 54, tag: 'backend', tagC: theme.violet },
    { name: 'Python · Cloud Run', kinds: ['source', 'build', 'push', 'deploy'], desc: 'Buildpacks → Cloud Run with traffic-split rollback.', uses: 39, tag: 'gcp', tagC: theme.good },
    { name: 'Monorepo · fan-out', kinds: ['source', 'build', 'build', 'test', 'deploy'], desc: 'Bounded fan-out builds across packages.', uses: 28, tag: 'advanced', tagC: theme.warn },
    { name: 'Approval · prod', kinds: ['source', 'build', 'push', 'approval', 'deploy'], desc: 'Human-in-the-loop gate before production jump.', uses: 21, tag: 'rbac', tagC: theme.bad },
  ];
  return (
    <AppShell theme={theme} active="Templates" crumbs={[{ label: 'Cooker' }, { label: 'Templates' }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        <CPageHeader theme={theme} eyebrow="catalog · create-from-template"
          title="Templates" subtitle="Pre-wired constellations. Pick one and Cooker deep-copies it with fresh IDs and re-validates the DAG — ready to launch in seconds."
          actions={<CBtn theme={theme} kind="primary">＋ New template</CBtn>} />
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(340px,1fr))', gap: 18 }}>
          {tpls.map((tp) => (
            <GlassCard key={tp.name} theme={theme} pad={18} style={{ cursor: 'pointer', position: 'relative', overflow: 'hidden' }}>
              <div style={{ position: 'absolute', right: -28, top: -28, width: 110, height: 110, borderRadius: 999,
                background: `radial-gradient(circle, ${hexA(tp.tagC, 0.16)}, transparent 70%)` }} />
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', position: 'relative' }}>
                <div style={{ fontFamily: theme.display, fontSize: 17, fontWeight: 600, color: theme.text }}>{tp.name}</div>
                <CPill theme={theme} color={tp.tagC}>{tp.tag}</CPill>
              </div>
              <div style={{ margin: '14px 0', position: 'relative' }}><ConstellationThumb theme={theme} kinds={tp.kinds} live={false} /></div>
              <p style={{ fontSize: 12.5, color: theme.textSoft, margin: '0 0 14px', lineHeight: 1.5, position: 'relative' }}>{tp.desc}</p>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', borderTop: `1px solid ${theme.line}`, paddingTop: 12, position: 'relative' }}>
                <span style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute }}>{tp.uses} deploys</span>
                <span style={{ fontFamily: theme.mono, fontSize: 11, color: theme.violet }}>use template →</span>
              </div>
            </GlassCard>
          ))}
        </div>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- Schedules
function SchedulesPage({ theme }) {
  const rows = [
    { name: 'search-indexer', cron: '0 3 * * *', human: 'daily · 03:00', next: 'in 4h 12m', tz: 'UTC', on: true },
    { name: 'billing-reconcile', cron: '*/30 * * * *', human: 'every 30 min', next: 'in 11m', tz: 'UTC', on: true },
    { name: 'cache-warm', cron: '0 */6 * * *', human: 'every 6 hours', next: 'in 2h 40m', tz: 'Asia/Bangkok', on: true },
    { name: 'weekly-report', cron: '0 9 * * 1', human: 'Mondays · 09:00', next: 'in 3d', tz: 'UTC', on: false },
  ];
  return (
    <AppShell theme={theme} active="Schedules" crumbs={[{ label: 'Cooker' }, { label: 'Schedules' }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        <CPageHeader theme={theme} eyebrow="cron · leader-elected"
          title="Schedules" subtitle="Cron-triggered runs, elected via a single leader so they fire exactly once across replicas. POSIX cron with IANA timezone support."
          actions={<CBtn theme={theme} kind="primary">＋ New schedule</CBtn>} />
        <GlassCard theme={theme} pad={0} style={{ overflow: 'hidden' }}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 150px 150px 130px 60px' }}>
            {['Pipeline', 'Cron', 'Cadence', 'Next run', ''].map((h, i) => (
              <div key={i} style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 1, textTransform: 'uppercase', color: theme.textMute, padding: '12px 16px' }}>{h}</div>
            ))}
          </div>
          {rows.map((r, i) => (
            <div key={r.name} style={{ display: 'grid', gridTemplateColumns: '1fr 150px 150px 130px 60px', alignItems: 'center',
              borderTop: `1px solid ${theme.line}`, background: i % 2 ? hexA(theme.violet, 0.03) : 'transparent', minHeight: 50, opacity: r.on ? 1 : 0.55 }}>
              <div style={{ padding: '12px 16px', display: 'flex', alignItems: 'center', gap: 11 }}>
                <PlanetOrb kind="custom" size={24} theme={theme} status={r.on && i === 1 ? 'running' : undefined} />
                <div><span style={{ fontFamily: theme.mono, fontSize: 12.5, color: theme.text, fontWeight: 600 }}>{r.name}</span>
                  <div style={{ fontFamily: theme.mono, fontSize: 9.5, color: theme.textMute, marginTop: 2 }}>{r.tz}</div></div>
              </div>
              <div style={{ padding: '12px 16px', fontFamily: theme.mono, fontSize: 12, color: theme.cyan }}>{r.cron}</div>
              <div style={{ padding: '12px 16px', fontFamily: theme.mono, fontSize: 11.5, color: theme.textSoft }}>{r.human}</div>
              <div style={{ padding: '12px 16px', fontFamily: theme.mono, fontSize: 11.5, color: r.on ? theme.ember : theme.textMute }}>{r.next}</div>
              <div style={{ padding: '12px 16px' }}>
                <span style={{ width: 30, height: 17, borderRadius: 999, position: 'relative', display: 'inline-block',
                  background: r.on ? theme.good : theme.line }}>
                  <span style={{ position: 'absolute', top: 2, left: r.on ? 15 : 2, width: 13, height: 13, borderRadius: 999, background: '#fff', transition: 'all .2s' }} />
                </span>
              </div>
            </div>
          ))}
        </GlassCard>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- Notifications
function NotificationsPage({ theme }) {
  const targets = [
    { name: '#deploys', kind: 'Slack', icon: 'S', c: '#4A154B', events: ['run.success', 'run.failed', 'deploy.*'], on: true },
    { name: 'ops-alerts', kind: 'Discord', icon: 'D', c: '#5865F2', events: ['run.failed', 'approval.required'], on: true },
    { name: 'team@acme.io', kind: 'Email', icon: '@', c: theme.cyan, events: ['run.failed'], on: true },
    { name: 'pagerduty-hook', kind: 'Webhook', icon: '↯', c: theme.ember, events: ['run.failed', 'host.keychanged'], on: false },
  ];
  return (
    <AppShell theme={theme} active="Notifications" crumbs={[{ label: 'Cooker' }, { label: 'Notifications' }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        <CPageHeader theme={theme} eyebrow="multi-channel dispatcher"
          title="Notifications" subtitle="Route run, deploy, and approval events to Slack, Discord, email, or a generic webhook — each target with its own event-type filters."
          actions={<CBtn theme={theme} kind="primary">＋ Add target</CBtn>} />
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill,minmax(420px,1fr))', gap: 16 }}>
          {targets.map((t) => (
            <GlassCard key={t.name} theme={theme} pad={18} style={{ opacity: t.on ? 1 : 0.6 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                <div style={{ width: 36, height: 36, borderRadius: 10, display: 'grid', placeItems: 'center', flexShrink: 0,
                  background: t.c, color: '#fff', fontFamily: theme.display, fontWeight: 700, fontSize: 16,
                  boxShadow: `0 0 18px ${hexA(typeof t.c === 'string' && t.c.startsWith('#') ? t.c : theme.cyan, 0.5)}` }}>{t.icon}</div>
                <div style={{ flex: 1 }}>
                  <div style={{ fontFamily: theme.mono, fontSize: 13.5, fontWeight: 600, color: theme.text }}>{t.name}</div>
                  <div style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute, marginTop: 2 }}>{t.kind}</div>
                </div>
                <span style={{ width: 30, height: 17, borderRadius: 999, position: 'relative', display: 'inline-block', background: t.on ? theme.good : theme.line }}>
                  <span style={{ position: 'absolute', top: 2, left: t.on ? 15 : 2, width: 13, height: 13, borderRadius: 999, background: '#fff' }} />
                </span>
              </div>
              <div style={{ marginTop: 14, paddingTop: 14, borderTop: `1px solid ${theme.line}`, display: 'flex', flexWrap: 'wrap', gap: 7 }}>
                {t.events.map((e) => <CPill key={e} theme={theme} color={theme.violet}>{e}</CPill>)}
              </div>
            </GlassCard>
          ))}
        </div>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- Settings
function SettingsPage({ theme }) {
  const Section = ({ title, children }) => (
    <GlassCard theme={theme} pad={22} style={{ marginBottom: 16 }}>
      <SLabel theme={theme} style={{ marginBottom: 16 }}>{title}</SLabel>
      {children}
    </GlassCard>
  );
  const Field = ({ label, value, pill, pillC }) => (
    <div style={{ display: 'flex', alignItems: 'center', gap: 14, padding: '11px 0', borderTop: `1px solid ${theme.line}` }}>
      <div style={{ width: 200, fontSize: 13, color: theme.text, fontWeight: 500 }}>{label}</div>
      <div style={{ flex: 1, fontFamily: theme.mono, fontSize: 12, color: theme.textSoft }}>{value}</div>
      {pill && <CPill theme={theme} color={pillC || theme.good}>{pill}</CPill>}
    </div>
  );
  return (
    <AppShell theme={theme} active="Settings" crumbs={[{ label: 'Cooker' }, { label: 'Settings' }]}>
      <div style={{ padding: '28px 30px 60px', maxWidth: 920, margin: '0 auto' }}>
        <CPageHeader theme={theme} eyebrow="workspace · config" title="Settings"
          subtitle="Boot configuration for this Cooker instance. Production mode runs a strict Config.Validate — misconfiguration fails loudly." />
        <Tabs theme={theme} tabs={['General', 'Builders', 'Secrets', 'Auth', 'Observability']} active="Builders" />
        <div style={{ marginTop: 18 }}>
          <Section title="Builder">
            <Field label="Image builder" value="kaniko" pill="default" pillC={theme.cyan} />
            <Field label="Pusher" value="crane" pill="OCI v1.1" pillC={theme.good} />
            <Field label="Max fan-out" value="8" />
            <Field label="Run deadline" value="30m" />
          </Section>
          <Section title="Secrets backend">
            <Field label="Backend" value="vault" pill="connected" pillC={theme.good} />
            <Field label="Mount" value="secret/data/cooker" />
            <Field label="AES key" value="·········· set" pill="encrypted" pillC={theme.violet} />
          </Section>
          <Section title="Observability">
            <Field label="Prometheus /metrics" value="enabled" pill="on" pillC={theme.good} />
            <Field label="OTLP traces" value="otel-collector:4317" pill="on" pillC={theme.good} />
            <Field label="Audit log" value="stdout" />
          </Section>
          <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
            <CBtn theme={theme} kind="secondary">Reset</CBtn>
            <CBtn theme={theme} kind="primary">Save configuration</CBtn>
          </div>
        </div>
      </div>
    </AppShell>
  );
}

Object.assign(window, { ComposePage, TemplatesPage, SchedulesPage, NotificationsPage, SettingsPage });
