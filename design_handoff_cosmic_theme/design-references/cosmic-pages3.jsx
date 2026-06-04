/* Cosmic Cooker — pages (3): App detail, New-app wizard, Clusters, Hosts, Docker */

// ----------------------------------------------------------------- App detail
function AppDetailPage({ theme }) {
  const envColor = { prod: theme.violet, staging: theme.cyan, dev: theme.cool };
  const deploys = [
    { v: '8f3c1a', env: 'prod', when: '2m ago', status: 'deploying', c: theme.ember, by: 'CK' },
    { v: '4a1b09', env: 'prod', when: '3h ago', status: 'deployed', c: theme.good, by: 'AS' },
    { v: 'd02f7e', env: 'staging', when: '5h ago', status: 'deployed', c: theme.good, by: 'CK' },
    { v: '91fd2c', env: 'staging', when: '1d ago', status: 'rolled back', c: theme.bad, by: 'JM' },
  ];
  return (
    <AppShell theme={theme} active="Apps" crumbs={[{ label: 'Cooker' }, { label: 'Apps' }, { label: 'checkout-api', mono: true }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        {/* hero */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 18, marginBottom: 24 }}>
          <PlanetOrb kind="deploy" size={56} status="running" theme={theme} ring />
          <div style={{ flex: 1 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <h1 style={{ fontFamily: theme.display, fontSize: 30, fontWeight: 600, color: theme.text, margin: 0, letterSpacing: -0.6 }}>checkout-api</h1>
              <CPill theme={theme} color={theme.ember}>● deploying</CPill>
              <CPill theme={theme} color={theme.violet}>prod</CPill>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 7, fontFamily: theme.mono, fontSize: 12, color: theme.textSoft }}>
              <span style={{ color: theme.cyan }}>↗ checkout.acme.io</span><span style={{ color: theme.textFaint }}>·</span>
              <span>github.com/acme/checkout</span>
            </div>
          </div>
          <CBtn theme={theme} kind="secondary">⟳ Redeploy</CBtn>
          <CBtn theme={theme} kind="primary">▶ Deploy</CBtn>
        </div>
        {/* stats */}
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4,1fr)', gap: 14, marginBottom: 22 }}>
          <Stat theme={theme} label="Health" value="99%" sub="all probes green" color={theme.good} />
          <Stat theme={theme} label="Replicas" value="3 / 3" sub="rollout 100%" color={theme.cyan} />
          <Stat theme={theme} label="Runs / 24h" value="14" sub="2 deploys" color={theme.violet} />
          <Stat theme={theme} label="p95 latency" value="42ms" sub="−6ms vs 1h" color={theme.good} />
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 360px', gap: 20 }}>
          {/* deploy history */}
          <GlassCard theme={theme} pad={0} style={{ overflow: 'hidden', alignSelf: 'start' }}>
            <div style={{ padding: '14px 18px', borderBottom: `1px solid ${theme.line}`, display: 'flex', alignItems: 'center', gap: 11 }}>
              <span style={{ fontFamily: theme.display, fontSize: 16, fontWeight: 600, color: theme.text }}>Deploy history</span>
              <CPill theme={theme} color={theme.textMute}>recent</CPill>
            </div>
            {deploys.map((d, i) => (
              <div key={i} style={{ display: 'grid', gridTemplateColumns: '30px 1fr 100px 90px 90px', alignItems: 'center', gap: 8,
                padding: '12px 18px', borderTop: i ? `1px solid ${theme.line}` : 'none' }}>
                <PlanetOrb kind="push" size={22} theme={theme} status={d.status === 'deploying' ? 'running' : undefined} />
                <span style={{ fontFamily: theme.mono, fontSize: 12.5, color: theme.text, fontWeight: 600 }}>{d.v}</span>
                <CPill theme={theme} color={envColor[d.env]}>{d.env}</CPill>
                <span style={{ fontFamily: theme.mono, fontSize: 11, color: d.c }}>{d.status}</span>
                <span style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute, textAlign: 'right' }}>{d.when}</span>
              </div>
            ))}
          </GlassCard>
          {/* right: config + webhook */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
            <GlassCard theme={theme} pad={18}>
              <SLabel theme={theme} style={{ marginBottom: 14 }}>Deploy target</SLabel>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14 }}>
                <KV theme={theme} k="Kind" v="kubernetes" color={theme.cyan} />
                <KV theme={theme} k="Namespace" v="cooker-prod" />
                <KV theme={theme} k="Builder" v="kaniko" />
                <KV theme={theme} k="Image" v="ghcr.io/acme/checkout" />
              </div>
            </GlassCard>
            <GlassCard theme={theme} pad={18}>
              <SLabel theme={theme} style={{ marginBottom: 14 }}>GitHub webhook</SLabel>
              <KV theme={theme} k="Endpoint" v="/webhooks/apps/8f3c…" />
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 14 }}>
                <span style={{ fontFamily: theme.mono, fontSize: 11.5, color: theme.textMute, flex: 1,
                  padding: '8px 10px', background: hexA('#000', 0.3), borderRadius: 8, border: `1px solid ${theme.line}` }}>hmac ········ a91f</span>
                <CBtn theme={theme} kind="secondary" style={{ padding: '8px 12px' }}>Rotate</CBtn>
              </div>
            </GlassCard>
          </div>
        </div>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- New-app wizard
function NewAppWizardPage({ theme }) {
  const steps = ['Connect repo', 'Pick target', 'Configure', 'Review & launch'];
  const active = 2;
  return (
    <AppShell theme={theme} active="Apps" crumbs={[{ label: 'Cooker' }, { label: 'Apps' }, { label: 'new', mono: true }]}>
      <div style={{ padding: '28px 30px 60px', maxWidth: 880, margin: '0 auto' }}>
        <CPageHeader theme={theme} eyebrow="launch sequence" title="New app"
          subtitle="Point Cooker at a repo, pick where each world should land, and we'll wire build → push → deploy end to end." />
        {/* stepper */}
        <div style={{ display: 'flex', alignItems: 'center', marginBottom: 28 }}>
          {steps.map((s, i) => (
            <React.Fragment key={s}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                <div style={{ width: 30, height: 30, borderRadius: 999, display: 'grid', placeItems: 'center', flexShrink: 0,
                  background: i < active ? theme.good : i === active ? `linear-gradient(135deg,${theme.violet},${theme.violetGlow})` : 'transparent',
                  border: i > active ? `1.5px solid ${theme.textFaint}` : 'none', color: '#fff', fontFamily: theme.mono, fontSize: 12, fontWeight: 700,
                  boxShadow: i === active ? `0 0 14px ${hexA(theme.violetGlow, 0.6)}` : 'none' }}>
                  {i < active ? '✓' : i + 1}</div>
                <span style={{ fontSize: 13, fontWeight: i === active ? 600 : 500, color: i <= active ? theme.text : theme.textMute }}>{s}</span>
              </div>
              {i < steps.length - 1 && <div style={{ flex: 1, height: 2, margin: '0 14px',
                background: i < active ? theme.good : `repeating-linear-gradient(90deg,${theme.textFaint} 0 4px,transparent 4px 9px)` }} />}
            </React.Fragment>
          ))}
        </div>
        <GlassCard theme={theme} pad={26}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 20 }}>
            <PlanetOrb kind="build" size={32} theme={theme} />
            <div><div style={{ fontFamily: theme.display, fontSize: 18, fontWeight: 600, color: theme.text }}>Configure the build</div>
              <div style={{ fontFamily: theme.mono, fontSize: 11, color: theme.textMute, marginTop: 2 }}>acme/checkout · main</div></div>
          </div>
          {[['App name', 'checkout-api'], ['Dockerfile path', './Dockerfile'], ['Build context', '.'], ['Exposed port', '8080']].map(([l, v]) => (
            <div key={l} style={{ marginBottom: 16 }}>
              <div style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 1, textTransform: 'uppercase', color: theme.textMute, marginBottom: 7 }}>{l}</div>
              <div style={{ padding: '11px 13px', borderRadius: 10, fontFamily: theme.mono, fontSize: 13, color: theme.text,
                background: hexA('#000', 0.28), border: `1px solid ${theme.line}` }}>{v}</div>
            </div>
          ))}
          <div style={{ display: 'flex', gap: 10, marginTop: 8 }}>
            <span style={{ flex: 1 }} />
            <CBtn theme={theme} kind="secondary">← Back</CBtn>
            <CBtn theme={theme} kind="primary">Continue →</CBtn>
          </div>
        </GlassCard>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- Clusters
function ClustersPage({ theme }) {
  const pods = [
    { name: 'checkout-api-7d9', ns: 'cooker-prod', status: 'Running', c: theme.good, cpu: 62, restarts: 0 },
    { name: 'billing-worker-2f', ns: 'cooker-prod', status: 'Running', c: theme.good, cpu: 41, restarts: 1 },
    { name: 'search-idx-cron-x9', ns: 'cooker-prod', status: 'Pending', c: theme.warn, cpu: 0, restarts: 0 },
    { name: 'auth-gw-rollout-a1', ns: 'cooker-staging', status: 'Running', c: theme.ember, cpu: 78, restarts: 0 },
    { name: 'imgr-55c-crash', ns: 'cooker-dev', status: 'CrashLoop', c: theme.bad, cpu: 4, restarts: 7 },
  ];
  return (
    <AppShell theme={theme} active="Clusters" crumbs={[{ label: 'Cooker' }, { label: 'Clusters' }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        <CPageHeader theme={theme} eyebrow="kubernetes · client-go watch"
          title="Clusters" subtitle="Live view of every workload orbiting your clusters — pods, namespaces, and rollout status streaming over a real-time watch."
          actions={<React.Fragment><CBtn theme={theme} kind="secondary">⬡ Add cluster</CBtn><CBtn theme={theme} kind="primary">⟳ Refresh</CBtn></React.Fragment>} />
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4,1fr)', gap: 14, marginBottom: 22 }}>
          <Stat theme={theme} label="Clusters" value="2" sub="prod · edge" color={theme.cyan} />
          <Stat theme={theme} label="Namespaces" value="6" color={theme.violet} />
          <Stat theme={theme} label="Pods running" value="38" sub="1 pending" color={theme.good} />
          <Stat theme={theme} label="Restarts / 1h" value="8" sub="1 crashloop" color={theme.bad} />
        </div>
        <GlassCard theme={theme} pad={0} style={{ overflow: 'hidden' }}>
          <div style={{ padding: '14px 18px', borderBottom: `1px solid ${theme.line}`, display: 'flex', alignItems: 'center', gap: 11 }}>
            <PlanetOrb kind="source" size={24} theme={theme} ring />
            <span style={{ fontFamily: theme.display, fontSize: 16, fontWeight: 600, color: theme.text }}>prod-cluster · workloads</span>
            <CPill theme={theme} color={theme.good}>● watch live</CPill>
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 150px 120px 90px 70px', padding: '0' }}>
            {['Pod', 'Namespace', 'Status', 'CPU', 'Restarts'].map((h) => (
              <div key={h} style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 1, textTransform: 'uppercase', color: theme.textMute, padding: '11px 16px' }}>{h}</div>
            ))}
          </div>
          {pods.map((p, i) => (
            <div key={p.name} style={{ display: 'grid', gridTemplateColumns: '1fr 150px 120px 90px 70px', alignItems: 'center',
              borderTop: `1px solid ${theme.line}`, background: i % 2 ? hexA(theme.violet, 0.03) : 'transparent', minHeight: 48 }}>
              <div style={{ padding: '11px 16px', display: 'flex', alignItems: 'center', gap: 10 }}>
                <StatusDot theme={theme} color={p.c} pulse={p.status === 'Running' && i === 3} />
                <span style={{ fontFamily: theme.mono, fontSize: 12, color: theme.text, fontWeight: 600 }}>{p.name}</span>
              </div>
              <div style={{ padding: '11px 16px', fontFamily: theme.mono, fontSize: 11, color: theme.textSoft }}>{p.ns}</div>
              <div style={{ padding: '11px 16px' }}><CPill theme={theme} color={p.c}>{p.status}</CPill></div>
              <div style={{ padding: '11px 16px', display: 'flex', alignItems: 'center', gap: 8 }}>
                <HealthBar theme={theme} value={p.cpu} /><span style={{ fontFamily: theme.mono, fontSize: 10.5, color: theme.textMute }}>{p.cpu}%</span>
              </div>
              <div style={{ padding: '11px 16px', fontFamily: theme.mono, fontSize: 12, color: p.restarts > 3 ? theme.bad : theme.textSoft }}>{p.restarts}</div>
            </div>
          ))}
        </GlassCard>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- Hosts
function HostsPage({ theme }) {
  const hosts = [
    { name: 'edge-fra-1', addr: '88.42.10.7', status: 'connected', c: theme.good, fp: 'SHA256:9b2f…e41', ctr: 6 },
    { name: 'edge-sgp-2', addr: '128.1.55.3', status: 'connected', c: theme.good, fp: 'SHA256:7a0d…c19', ctr: 4 },
    { name: 'lab-box', addr: '10.0.0.12', status: 'key changed', c: theme.bad, fp: 'SHA256:31bc…88a', ctr: 0 },
  ];
  return (
    <AppShell theme={theme} active="Environments" crumbs={[{ label: 'Cooker' }, { label: 'Hosts' }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        <CPageHeader theme={theme} eyebrow="ssh-docker · tofu pinned"
          title="Hosts" subtitle="Register a box, paste its key once, and Cooker SSHes in to pull & run containers. Host keys are pinned on first connect — a changed key locks the world out."
          actions={<CBtn theme={theme} kind="primary">＋ Register host</CBtn>} />
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(340px,1fr))', gap: 16 }}>
          {hosts.map((h) => (
            <GlassCard key={h.name} theme={theme} pad={18} style={{ borderColor: h.c === theme.bad ? hexA(theme.bad, 0.4) : theme.line }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                <PlanetOrb kind="source" size={34} status={h.c === theme.bad ? undefined : undefined} theme={theme} />
                <div style={{ flex: 1 }}>
                  <div style={{ fontFamily: theme.mono, fontSize: 14, fontWeight: 600, color: theme.text }}>{h.name}</div>
                  <div style={{ fontFamily: theme.mono, fontSize: 11, color: theme.textMute, marginTop: 2 }}>{h.addr}</div>
                </div>
                <CPill theme={theme} color={h.c}>{h.status}</CPill>
              </div>
              <div style={{ marginTop: 16, paddingTop: 14, borderTop: `1px solid ${theme.line}`, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <KV theme={theme} k="Host key" v={h.fp} color={h.c === theme.bad ? theme.bad : theme.textSoft} />
                <span style={{ fontFamily: theme.mono, fontSize: 11, color: theme.textSoft }}>{h.ctr} containers</span>
              </div>
              {h.c === theme.bad && <div style={{ marginTop: 12 }}><CBtn theme={theme} kind="danger" style={{ width: '100%' }}>⚠ Re-verify host key</CBtn></div>}
            </GlassCard>
          ))}
        </div>
      </div>
    </AppShell>
  );
}

// ----------------------------------------------------------------- Docker
function DockerPage({ theme }) {
  const ctr = [
    { name: 'cooker-api', img: 'ghcr.io/acme/checkout:8f3c', status: 'up 2h', c: theme.good, port: '8080→8080' },
    { name: 'cooker-redis', img: 'redis:7-alpine', status: 'up 2h', c: theme.good, port: '6379' },
    { name: 'cooker-pg', img: 'postgres:16', status: 'up 2h', c: theme.good, port: '5432' },
    { name: 'cooker-builder', img: 'gcr.io/kaniko-project/executor', status: 'exited (0)', c: theme.textMute, port: '—' },
  ];
  return (
    <AppShell theme={theme} active="Apps" crumbs={[{ label: 'Cooker' }, { label: 'Docker' }]}>
      <div style={{ padding: '28px 30px 60px' }}>
        <CPageHeader theme={theme} eyebrow="local engine · docker.sock"
          title="Docker" subtitle="Inspect containers, images, and volumes on the local engine. Dev-only — refused at boot in production."
          actions={<CBtn theme={theme} kind="secondary">⟳ Prune</CBtn>} />
        <Tabs theme={theme} tabs={['Containers', 'Images', 'Volumes', 'Networks']} active="Containers" />
        <GlassCard theme={theme} pad={0} style={{ overflow: 'hidden', marginTop: 18 }}>
          <div style={{ display: 'grid', gridTemplateColumns: '32px 1fr 1fr 130px 120px', padding: '0' }}>
            {['', 'Container', 'Image', 'Ports', 'Status'].map((h, i) => (
              <div key={i} style={{ fontFamily: theme.mono, fontSize: 10, letterSpacing: 1, textTransform: 'uppercase', color: theme.textMute, padding: '11px 14px' }}>{h}</div>
            ))}
          </div>
          {ctr.map((c, i) => (
            <div key={c.name} style={{ display: 'grid', gridTemplateColumns: '32px 1fr 1fr 130px 120px', alignItems: 'center',
              borderTop: `1px solid ${theme.line}`, background: i % 2 ? hexA(theme.violet, 0.03) : 'transparent', minHeight: 48 }}>
              <div style={{ padding: '11px 14px' }}><StatusDot theme={theme} color={c.c} /></div>
              <div style={{ padding: '11px 14px', fontFamily: theme.mono, fontSize: 12, color: theme.text, fontWeight: 600 }}>{c.name}</div>
              <div style={{ padding: '11px 14px', fontFamily: theme.mono, fontSize: 11, color: theme.textSoft, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{c.img}</div>
              <div style={{ padding: '11px 14px', fontFamily: theme.mono, fontSize: 11, color: theme.cyan }}>{c.port}</div>
              <div style={{ padding: '11px 14px', fontFamily: theme.mono, fontSize: 11, color: c.c }}>{c.status}</div>
            </div>
          ))}
        </GlassCard>
      </div>
    </AppShell>
  );
}

Object.assign(window, { AppDetailPage, NewAppWizardPage, ClustersPage, HostsPage, DockerPage });
