// Cooker — design tokens
// Aesthetic: warm "workshop" — paper + ink + rust accent, with a strict mono data layer.

export const COOKER_TOKENS = {
  // surfaces (light "paper" theme)
  paper: '#F5EFE4',
  paperAlt: '#EDE5D4',
  paperDeep: '#E2D6BC',
  ink: '#1B1714',
  inkSoft: '#3A322C',
  inkMute: '#7A6E62',
  inkFaint: '#B5A993',

  // dark theme variants
  coal: '#16130F',
  coalAlt: '#1F1B16',
  coalDeep: '#2B2620',
  bone: '#F2EBDD',
  boneSoft: '#D7CDB8',
  boneMute: '#8E8270',

  // single brand accent — rust/copper, inspired by "Cooker"
  rust: '#C2410C',
  rustDeep: '#9A2F08',
  ember: '#F59E0B',

  // status (kept tonally aligned w/ paper palette — not pure web-RGB)
  good: '#3F7D58',
  warn: '#B7791F',
  bad: '#A03A2C',
  cool: '#3A6B8A',

  // typography stacks
  mono: '"JetBrains Mono", "IBM Plex Mono", ui-monospace, monospace',
  serif: '"Fraunces", "Source Serif 4", Georgia, serif',
  sans: '"Inter Tight", "Inter", system-ui, sans-serif',
} as const;

export type ThemeMode = 'light' | 'dark';

export interface CookerTheme {
  bg: string;
  surface: string;
  surfaceAlt: string;
  line: string;
  lineSoft: string;
  text: string;
  textSoft: string;
  textMute: string;
  accent: string;
  accentDeep: string;
  ember: string;
  good: string;
  warn: string;
  bad: string;
  cool: string;
  mono: string;
  serif: string;
  sans: string;
}

export function cookerTheme(mode: ThemeMode = 'dark'): CookerTheme {
  const T = COOKER_TOKENS;
  if (mode === 'dark') {
    return {
      bg: T.coal,
      surface: T.coalAlt,
      surfaceAlt: T.coalDeep,
      line: '#3A332A',
      lineSoft: '#2B2620',
      text: T.bone,
      textSoft: T.boneSoft,
      textMute: T.boneMute,
      accent: '#E8703A',
      accentDeep: T.rust,
      ember: T.ember,
      good: '#7BB48A',
      warn: '#E0B14B',
      bad: '#D87060',
      cool: '#7BA8C8',
      mono: T.mono,
      serif: T.serif,
      sans: T.sans,
    };
  }
  return {
    bg: T.paper,
    surface: '#FBF7EE',
    surfaceAlt: T.paperAlt,
    line: T.paperDeep,
    lineSoft: '#D9CFB8',
    text: T.ink,
    textSoft: T.inkSoft,
    textMute: T.inkMute,
    accent: T.rust,
    accentDeep: T.rustDeep,
    ember: T.ember,
    good: T.good,
    warn: T.warn,
    bad: T.bad,
    cool: T.cool,
    mono: T.mono,
    serif: T.serif,
    sans: T.sans,
  };
}

// hex with alpha helper
export function hexA(hex: string, a: number): string {
  const h = hex.replace('#', '');
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  return `rgba(${r},${g},${b},${a})`;
}
