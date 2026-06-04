/* Cosmic Cooker — theme tokens
   Re-skin of Cooker (CI/CD DAG editor) into a galaxy identity.
   Concept: each pipeline stage is a PLANET; each edge is a TRAJECTORY
   linking one planet to another. Status maps to cosmic phenomena —
   "running" keeps Cooker's warm ember/gold as star-fire.            */

// ---- DARK: "Deep Field" — the night sky ---------------------------------
const THEME_DARK = {
  mode: 'dark',
  name: 'Deep Field',
  // void / canvas
  void: '#06061a',
  bg: '#08081f',
  canvasTop: '#0c0b28',
  canvasBot: '#070617',
  // panels
  surface: 'rgba(20, 21, 52, 0.72)',
  surfaceSolid: '#12132f',
  surfaceAlt: '#0d0e25',
  panelGlass: 'rgba(14, 15, 40, 0.66)',
  // lines
  line: 'rgba(138, 126, 230, 0.16)',
  lineStrong: 'rgba(138, 126, 230, 0.32)',
  // text
  text: '#ECEBFF',
  textSoft: '#ADB0E4',
  textMute: '#6E70A6',
  textFaint: '#4A4C7A',
  // accents
  violet: '#8B6DFF',
  violetGlow: '#7C5CFF',
  cyan: '#33E1F2',
  ember: '#FFB454',   // running / active (Cooker continuity)
  emberDeep: '#F59E0B',
  good: '#4FE6B0',
  bad: '#FF6B8A',
  warn: '#FFC861',
  cool: '#5BB6FF',
  // type
  display: '"Space Grotesk", "Inter Tight", system-ui, sans-serif',
  sans: '"Inter Tight", "Inter", system-ui, sans-serif',
  mono: '"JetBrains Mono", ui-monospace, monospace',
  // starfield strength
  stars: 0.9,
};

// ---- LIGHT: "Daybreak" — dawn atmosphere over the horizon ---------------
const THEME_DAWN = {
  mode: 'light',
  name: 'Daybreak',
  void: '#eef1fb',
  bg: '#f3eef0',
  canvasTop: '#e9eefc',   // soft upper sky
  canvasBot: '#fdeee4',   // peach horizon
  surface: 'rgba(255, 255, 255, 0.74)',
  surfaceSolid: '#ffffff',
  surfaceAlt: '#f4f1fb',
  panelGlass: 'rgba(255, 255, 255, 0.7)',
  line: 'rgba(108, 96, 196, 0.18)',
  lineStrong: 'rgba(108, 96, 196, 0.34)',
  text: '#241f3c',
  textSoft: '#5a5680',
  textMute: '#928fb4',
  textFaint: '#bdbad6',
  violet: '#6D52E0',
  violetGlow: '#7C5CFF',
  cyan: '#1597B6',
  ember: '#E8902F',
  emberDeep: '#C9740F',
  good: '#1FA97A',
  bad: '#E05574',
  warn: '#D99411',
  cool: '#3B82C4',
  display: '"Space Grotesk", "Inter Tight", system-ui, sans-serif',
  sans: '"Inter Tight", "Inter", system-ui, sans-serif',
  mono: '"JetBrains Mono", ui-monospace, monospace',
  stars: 0.16,
};

// Planet identity per stage type. `from`/`to` = orb gradient stops, `glow` = halo.
const PLANET_KIND = {
  source:   { ch: '◐', from: '#7FD0FF', to: '#3B82C4', glow: '#5BB6FF', label: 'Source' },
  build:    { ch: '✦', from: '#FFC36B', to: '#E8702A', glow: '#FF9D45', label: 'Build'  },
  test:     { ch: '◉', from: '#7CF2FF', to: '#1AA7C4', glow: '#33E1F2', label: 'Test'   },
  push:     { ch: '▲', from: '#C0A8FF', to: '#7C5CFF', glow: '#A78BFA', label: 'Push'   },
  deploy:   { ch: '⬢', from: '#7DF4C6', to: '#19A878', glow: '#4FE6B0', label: 'Deploy' },
  approval: { ch: '◇', from: '#FFE08A', to: '#E0A93A', glow: '#FFD166', label: 'Approval' },
  custom:   { ch: '✺', from: '#D7B9FF', to: '#9B6BE8', glow: '#C49BFF', label: 'Custom' },
};

// status → tone color
function statusColor(theme, status) {
  switch (status) {
    case 'success':
    case 'deployed': return theme.good;
    case 'failed':
    case 'error':    return theme.bad;
    case 'running':
    case 'building': return theme.ember;
    case 'pending':
    case 'queued':   return theme.textMute;
    case 'awaiting': return theme.warn;
    default:         return theme.textMute;
  }
}

function hexA(hex, a) {
  const h = hex.replace('#', '');
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  return `rgba(${r},${g},${b},${a})`;
}

Object.assign(window, { THEME_DARK, THEME_DAWN, PLANET_KIND, statusColor, hexA });
