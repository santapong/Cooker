/* Cosmic Cooker — inspector, canvas chrome, full screen + explorations */

// ----------------------------------------------------------------- Inspector
function Inspector({ theme, node }) {
  const k = PLANET_KIND[node.kind] || PLANET_KIND.custom;
  const Row = ({ label, value, mono = true }) => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4, marginBottom: 13 }}>
      <span style={{ fontFamily: theme.mono, fontSize: 9.5, letterSpacing: 1.2, textTransform: 'uppercase',
        color: theme.textMute }}>{label}</span>
      <span style={{ fontFamily: mono ? theme.mono : theme.sans, fontSize: 12, color: theme.text,
        wordBreak: 'break-all' }}>{value}</span>
    </div>
  );
  return (
    <aside style={{ width: 296, flexShrink: 0, position: 'relative', zIndex: 2,
      borderLeft: `1px solid ${theme.line}`, background: theme.panelGlass,
      backdropFilter: 'blur(14px)', WebkitBackdropFilter: 'blur(14px)',
      display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* header */}
      <div style={{ padding: '16px 17px 14px', borderBottom: `1px solid ${theme.line}`,
        display: 'flex', alignItems: 'center', gap: 12 }}>
        <PlanetOrb kind={node.kind} size={40} status={node.status} theme={theme} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontFamily: theme.display, fontSize: 16, fontWeight: 600, color: theme.text,
            letterSpacing: -0.2 }}>{node.label}</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 4 }}>
            <span style={{ fontFamily: theme.mono, fontSize: 9.5, letterSpacing: 1, textTransform: 'uppercase',
              color: k.glow }}>{k.label}</span>
            <span style={{ color: theme.textFaint }}>·</span>
            <span style={{ fontFamily: theme.mono, fontSize: 9.5, letterSpacing: 1, textTransform: 'uppercase',
              color: theme.ember }}>running</span>
          </div>
        </div>
      </div>
      {/* body */}
      <div style={{ padding: '15px 17px', flex: 1, overflow: 'hidden' }}>
        <Row label="Image" value="ghcr.io/acme/checkout:8f3c1a" />
        <Row label="Command" value="go test ./... -race -count=1" />
        <div style={{ display: 'flex', gap: 16 }}>
          <div style={{ flex: 1 }}><Row label="Timeout" value="15m" /></div>
          <div style={{ flex: 1 }}><Row label="Retries" value="2" /></div>
          <div style={{ flex: 1 }}><Row label="Fan-out" value="8" /></div>
        </div>
        {/* live log tail */}
        <div style={{ marginTop: 4 }}>
          <span style={{ fontFamily: theme.mono, fontSize: 9.5, letterSpacing: 1.2, textTransform: 'uppercase',
            color: theme.textMute }}>Live telemetry</span>
          <div style={{ marginTop: 7, padding: '10px 11px', borderRadius: 9, fontFamily: theme.mono,
            fontSize: 10.5, lineHeight: 1.7, background: theme.mode === 'dark' ? hexA('#000', 0.34) : hexA('#1a1730', 0.04),
            border: `1px solid ${theme.line}`, color: theme.textSoft }}>
            <div><span style={{ color: theme.good }}>ok</span> pkg/dagrunner 0.42s</div>
            <div><span style={{ color: theme.good }}>ok</span> internal/oci 1.18s</div>
            <div><span style={{ color: theme.ember }}>run</span> internal/service
              <span style={{ display: 'inline-block', width: 6, height: 11, marginLeft: 3, verticalAlign: -1,
                background: theme.ember, animation: 'ccBlink 1s step-end infinite' }} /></div>
          </div>
        </div>
      </div>
      {/* footer */}
      <div style={{ padding: '12px 17px', borderTop: `1px solid ${theme.line}`, display: 'flex', gap: 8 }}>
        <button style={{ flex: 1, padding: '8px 12px', borderRadius: 9, fontSize: 12, fontWeight: 600,
          fontFamily: theme.sans, cursor: 'pointer', background: theme.surface, color: theme.text,
          border: `1px solid ${theme.line}` }}>Logs</button>
        <button style={{ flex: 1, padding: '8px 12px', borderRadius: 9, fontSize: 12, fontWeight: 600,
          fontFamily: theme.sans, cursor: 'pointer', color: theme.bad, background: hexA(theme.bad, 0.12),
          border: `1px solid ${hexA(theme.bad, 0.4)}` }}>Abort</button>
      </div>
    </aside>
  );
}

// ----------------------------------------------------------------- Canvas chrome
function CanvasControls({ theme }) {
  const Btn = ({ ch }) => (
    <button style={{ width: 30, height: 30, display: 'grid', placeItems: 'center', cursor: 'pointer',
      background: theme.panelGlass, color: theme.textSoft, border: `1px solid ${theme.line}`,
      backdropFilter: 'blur(8px)', fontSize: 14, fontFamily: theme.mono }}>{ch}</button>
  );
  return (
    <div style={{ position: 'absolute', top: 16, right: 16, zIndex: 4, display: 'flex',
      flexDirection: 'column', borderRadius: 10, overflow: 'hidden',
      boxShadow: `0 8px 24px ${hexA('#000', 0.3)}` }}>
      <Btn ch="+" /><Btn ch="−" /><Btn ch="⤢" />
    </div>
  );
}

function Minimap({ theme, nodes }) {
  const W = 150, H = 92, scale = 0.1;
  return (
    <div style={{ position: 'absolute', bottom: 16, left: 16, zIndex: 4, width: W, height: H,
      borderRadius: 10, overflow: 'hidden', background: theme.panelGlass,
      border: `1px solid ${theme.line}`, backdropFilter: 'blur(8px)',
      boxShadow: `0 8px 24px ${hexA('#000', 0.3)}` }}>
      <Starfield theme={theme} seed={3} density={18} nebula={false} />
      {nodes.map((n) => {
        const k = PLANET_KIND[n.kind] || PLANET_KIND.custom;
        const live = n.status === 'running';
        return <span key={n.id} style={{ position: 'absolute', left: 8 + n.x * scale, top: 14 + n.y * scale,
          width: 7, height: 7, borderRadius: 999,
          background: `radial-gradient(circle at 35% 30%, ${k.from}, ${k.to})`,
          boxShadow: `0 0 ${live ? 7 : 3}px ${live ? theme.ember : k.glow}` }} />;
      })}
      <div style={{ position: 'absolute', left: 6, top: 8, width: 78, height: 60,
        border: `1px solid ${hexA(theme.violet, 0.6)}`, borderRadius: 4,
        background: hexA(theme.violet, 0.08) }} />
    </div>
  );
}

// ----------------------------------------------------------------- Graph data
const PIPELINE_NODES = [
  { id: 'clone',  kind: 'source',   label: 'clone repo',  detail: 'github.com/acme/checkout', status: 'success', duration: '3.1s' },
  { id: 'build',  kind: 'build',    label: 'build image', detail: 'Kaniko · linux/amd64',     status: 'success', duration: '47s', x: 0 },
  { id: 'scan',   kind: 'custom',   label: 'sast scan',   detail: 'trivy · CRITICAL=0',       status: 'success', duration: '12s' },
  { id: 'test',   kind: 'test',     label: 'unit tests',  detail: 'go test ./... -race',       status: 'running', duration: '00:38' },
  { id: 'push',   kind: 'push',     label: 'push image',  detail: 'ghcr.io · oci v1.1',        status: 'pending' },
  { id: 'deploy', kind: 'deploy',   label: 'deploy',      detail: 'k8s · rollout',             status: 'pending', env: 'staging' },
];
// layout coords (top-left within canvas content space)
const LAYOUT = {
  clone:  { x: 16,  y: 222 },
  build:  { x: 246, y: 222 },
  test:   { x: 476, y: 124 },
  scan:   { x: 476, y: 320 },
  push:   { x: 706, y: 222 },
  deploy: { x: 936, y: 222 },
};
const PIPELINE_EDGES = [
  { from: 'clone', to: 'build', state: 'done' },
  { from: 'build', to: 'test',  state: 'active' },
  { from: 'build', to: 'scan',  state: 'done' },
  { from: 'test',  to: 'push',  state: 'idle' },
  { from: 'scan',  to: 'push',  state: 'idle' },
  { from: 'push',  to: 'deploy', state: 'idle' },
];

function buildNodes() {
  return PIPELINE_NODES.map((n) => ({ ...n, ...LAYOUT[n.id] }));
}

// ----------------------------------------------------------------- Full editor
function PipelineEditor({ theme, w, h }) {
  const nodes = buildNodes();
  return (
    <div style={{ width: w, height: h, display: 'flex', position: 'relative', overflow: 'hidden',
      fontFamily: theme.sans, background: theme.bg, color: theme.text }}>
      <CosmicSidebar theme={theme} />
      <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
        <EditorHeader theme={theme} />
        <div style={{ flex: 1, minHeight: 0, display: 'flex', position: 'relative' }}>
          <Palette theme={theme} />
          {/* canvas */}
          <div style={{ flex: 1, position: 'relative', overflow: 'hidden',
            background: `radial-gradient(130% 120% at 30% 0%, ${theme.canvasTop} 0%, ${theme.canvasBot} 70%)` }}>
            <Starfield theme={theme} seed={42} density={theme.mode === 'dark' ? 130 : 30} />
            {/* faint grid */}
            <div style={{ position: 'absolute', inset: 0, opacity: theme.mode === 'dark' ? 0.5 : 0.7,
              backgroundImage: `radial-gradient(${hexA(theme.violet, theme.mode === 'dark' ? 0.1 : 0.12)} 1px, transparent 1px)`,
              backgroundSize: '34px 34px' }} />
            <CosmicGraph theme={theme} nodes={nodes} edges={PIPELINE_EDGES} selectedId="test"
              width={1100} height={620} />
            <CanvasControls theme={theme} />
            <Minimap theme={theme} nodes={nodes} />
            {/* hint */}
            <div style={{ position: 'absolute', bottom: 18, right: 18, zIndex: 4, fontFamily: theme.mono,
              fontSize: 10, color: theme.textMute, letterSpacing: 0.5 }}>drag a planet to launch · scroll to warp →</div>
          </div>
          <Inspector theme={theme} node={nodes.find((n) => n.id === 'test')} />
        </div>
      </div>
    </div>
  );
}

Object.assign(window, { Inspector, CanvasControls, Minimap, PipelineEditor, buildNodes,
  PIPELINE_NODES, PIPELINE_EDGES, LAYOUT });
