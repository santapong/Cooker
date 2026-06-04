// Cooker — design tokens
// Aesthetic: "Cosmic / Galaxy". Each pipeline stage is a planet, each DAG edge
// a trajectory between planets. Status maps to cosmic phenomena, and Cooker's
// warm ember/gold is kept as the "running / live" star-fire for brand continuity.
//
// Two modes share one token contract:
//   - Deep Field (dark, primary) — deep-space navy, nebula bloom, violet→cyan glow.
//   - Daybreak  (light)          — dawn atmosphere (peach horizon → sky blue).

export const COOKER_TOKENS = {
  // legacy "workshop" surfaces — retained so any non-themed reference still resolves
  paper: '#F5EFE4',
  paperAlt: '#EDE5D4',
  paperDeep: '#E2D6BC',
  ink: '#1B1714',
  inkSoft: '#3A322C',
  inkMute: '#7A6E62',
  inkFaint: '#B5A993',
  coal: '#16130F',
  coalAlt: '#1F1B16',
  coalDeep: '#2B2620',
  bone: '#F2EBDD',
  boneSoft: '#D7CDB8',
  boneMute: '#8E8270',
  rust: '#C2410C',
  rustDeep: '#9A2F08',
  ember: '#F59E0B',
  good: '#3F7D58',
  warn: '#B7791F',
  bad: '#A03A2C',
  cool: '#3A6B8A',

  // typography stacks — display is now Space Grotesk (replaces Fraunces)
  mono: '"JetBrains Mono", "IBM Plex Mono", ui-monospace, monospace',
  display: '"Space Grotesk", "Inter Tight", system-ui, sans-serif',
  // `serif` slot retained for back-compat; repointed at the display stack so
  // existing `t.serif` headings flip to Space Grotesk with no per-call changes.
  serif: '"Space Grotesk", "Inter Tight", system-ui, sans-serif',
  sans: '"Inter Tight", "Inter", system-ui, sans-serif',
} as const;

export type ThemeMode = 'light' | 'dark';

// ── Planet identity per stage `kind` ───────────────────────────────────────
// `from`/`to` are the orb radial-gradient stops, `glow` the halo, `ch` the glyph.
// Theme-independent: the planets read the same in Deep Field and Daybreak.
export type PlanetKind =
  | 'source'
  | 'build'
  | 'test'
  | 'push'
  | 'deploy'
  | 'approval'
  | 'custom';

export interface PlanetStyle {
  ch: string;
  from: string;
  to: string;
  glow: string;
  label: string;
}

// The live-log "console" surface is deliberately near-black in BOTH modes
// (it reads as a terminal). Centralised here so the values live in one place
// rather than as magic hexes at page altitude.
export const CONSOLE = {
  bg: '#05050F',
  text: '#D6D8FF',
  dim: '#ADB0E4',
  faint: '#5A5C8A',
} as const;

export const PLANET_KINDS: Record<PlanetKind, PlanetStyle> = {
  source: { ch: '◐', from: '#7FD0FF', to: '#3B82C4', glow: '#5BB6FF', label: 'Source' },
  build: { ch: '✦', from: '#FFC36B', to: '#E8702A', glow: '#FF9D45', label: 'Build' },
  test: { ch: '◉', from: '#7CF2FF', to: '#1AA7C4', glow: '#33E1F2', label: 'Test' },
  push: { ch: '▲', from: '#C0A8FF', to: '#7C5CFF', glow: '#A78BFA', label: 'Push' },
  deploy: { ch: '⬢', from: '#7DF4C6', to: '#19A878', glow: '#4FE6B0', label: 'Deploy' },
  approval: { ch: '◇', from: '#FFE08A', to: '#E0A93A', glow: '#FFD166', label: 'Approval' },
  custom: { ch: '✺', from: '#D7B9FF', to: '#9B6BE8', glow: '#C49BFF', label: 'Custom' },
};

export interface CookerTheme {
  mode: ThemeMode; // 'dark' = Deep Field, 'light' = Daybreak
  // ── canvas / surfaces ──
  void: string; // page/root background, letterbox
  bg: string; // app background
  canvasTop: string; // DAG canvas radial gradient — top stop
  canvasBot: string; // DAG canvas radial gradient — bottom stop
  surface: string; // glass panels (pair with backdrop-filter: blur())
  surfaceSolid: string; // opaque cards/rows
  surfaceAlt: string; // sidebar / recessed
  panelGlass: string; // topbar / palette / inspector glass
  // ── lines ──
  line: string; // hairline borders (accent-tinted)
  lineStrong: string; // stronger dividers
  lineSoft: string; // faintest separators
  // ── text ──
  text: string; // primary (starlight)
  textSoft: string; // secondary
  textMute: string; // tertiary / mono labels
  textFaint: string; // disabled / separators
  // ── accents ──
  accent: string; // primary accent (= violet) — back-compat alias
  accentDeep: string; // deep accent (= violetGlow) — back-compat alias
  violet: string; // primary accent
  violetGlow: string; // glows, focus rings, primary-btn gradient end
  cyan: string; // secondary accent
  ember: string; // running / live (Cooker continuity)
  emberDeep: string; // ember gradient end
  good: string; // success / deployed
  warn: string; // pending / awaiting
  bad: string; // failed / error
  cool: string; // info
  // ── type ──
  display: string; // headings (Space Grotesk)
  serif: string; // back-compat alias of display
  sans: string; // body / UI
  mono: string; // data
  // ── cosmic extras ──
  planets: Record<PlanetKind, PlanetStyle>; // per-kind planet identity
  stars: number; // starfield strength (0..1)
}

export function cookerTheme(mode: ThemeMode = 'dark'): CookerTheme {
  const T = COOKER_TOKENS;
  if (mode === 'dark') {
    // ── Deep Field (primary) ──
    return {
      mode,
      void: '#06061A',
      bg: '#08081F',
      canvasTop: '#0C0B28',
      canvasBot: '#070617',
      surface: 'rgba(20, 21, 52, 0.72)',
      surfaceSolid: '#12132F',
      surfaceAlt: '#0D0E25',
      panelGlass: 'rgba(14, 15, 40, 0.66)',
      line: 'rgba(138, 126, 230, 0.16)',
      lineStrong: 'rgba(138, 126, 230, 0.32)',
      lineSoft: 'rgba(138, 126, 230, 0.10)',
      text: '#ECEBFF',
      textSoft: '#ADB0E4',
      textMute: '#6E70A6',
      textFaint: '#4A4C7A',
      accent: '#8B6DFF',
      accentDeep: '#7C5CFF',
      violet: '#8B6DFF',
      violetGlow: '#7C5CFF',
      cyan: '#33E1F2',
      ember: '#FFB454',
      emberDeep: '#F59E0B',
      good: '#4FE6B0',
      warn: '#FFC861',
      bad: '#FF6B8A',
      cool: '#5BB6FF',
      display: T.display,
      serif: T.serif,
      sans: T.sans,
      mono: T.mono,
      planets: PLANET_KINDS,
      stars: 0.9,
    };
  }
  // ── Daybreak (light) ──
  return {
    mode,
    void: '#EEF1FB',
    bg: '#F3EEF0',
    canvasTop: '#E9EEFC',
    canvasBot: '#FDEEE4',
    surface: 'rgba(255, 255, 255, 0.74)',
    surfaceSolid: '#FFFFFF',
    surfaceAlt: '#F4F1FB',
    panelGlass: 'rgba(255, 255, 255, 0.70)',
    line: 'rgba(108, 96, 196, 0.18)',
    lineStrong: 'rgba(108, 96, 196, 0.34)',
    lineSoft: 'rgba(108, 96, 196, 0.10)',
    text: '#241F3C',
    textSoft: '#5A5680',
    textMute: '#928FB4',
    textFaint: '#BDBAD6',
    accent: '#6D52E0',
    accentDeep: '#7C5CFF',
    violet: '#6D52E0',
    violetGlow: '#7C5CFF',
    cyan: '#1597B6',
    ember: '#E8902F',
    emberDeep: '#C9740F',
    good: '#1FA97A',
    warn: '#D99411',
    bad: '#E05574',
    cool: '#3B82C4',
    display: T.display,
    serif: T.serif,
    sans: T.sans,
    mono: T.mono,
    planets: PLANET_KINDS,
    stars: 0.16,
  };
}

// Color-with-alpha helper. Accepts both `#rrggbb` hex AND `rgb()/rgba()`
// strings — several cosmic tokens (line, lineStrong, lineSoft, surface,
// panelGlass) are authored as rgba(), and callers pass them straight in to
// re-tint at a new alpha, so a hex-only parser would yield rgba(NaN,…).
export function hexA(color: string, a: number): string {
  const c = color.trim();
  const rgb = c.match(/^rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)/i);
  if (rgb) {
    return `rgba(${rgb[1]},${rgb[2]},${rgb[3]},${a})`;
  }
  const h = c.replace('#', '');
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  return `rgba(${r},${g},${b},${a})`;
}
