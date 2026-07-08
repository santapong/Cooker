import { PLANET_KINDS, type CookerTheme, type PlanetKind } from '../../theme/tokens';

// Pure tone/status/formatting helpers shared by the ui/ primitives. No JSX
// here on purpose — this module stays fast-refresh friendly (keeping
// non-component exports out of the component files under this folder).

export type Tone = 'neutral' | 'accent' | 'good' | 'warn' | 'bad' | 'cool' | 'ember';

// tone → base accent color, resolved against the active (cosmic) theme
export function toneColor(t: CookerTheme, tone: Tone): string {
  const map: Record<Tone, string> = {
    neutral: t.textMute,
    accent: t.violet,
    good: t.good,
    warn: t.warn,
    bad: t.bad,
    cool: t.cool,
    ember: t.ember,
  };
  return map[tone];
}

export type BtnKind = 'primary' | 'secondary' | 'ghost' | 'ink' | 'danger';

// normalize incoming kind strings (e.g. 'approve', 'docker_build') to a PlanetKind
export function planetKindOf(kind?: string): PlanetKind {
  const k = (kind || '').toLowerCase();
  if (k in PLANET_KINDS) return k as PlanetKind;
  if (k.startsWith('approv')) return 'approval';
  if (k.includes('build')) return 'build';
  if (k.includes('test')) return 'test';
  if (k.includes('push') || k.includes('registry')) return 'push';
  if (k.includes('deploy')) return 'deploy';
  if (k.includes('source') || k.includes('clone') || k.includes('checkout')) return 'source';
  return 'custom';
}

export function statusTone(status?: string): Tone {
  switch (status) {
    case 'success':
    case 'deployed':
    case 'good':
    case 'ok':
      return 'good';
    case 'failed':
    case 'error':
    case 'bad':
      return 'bad';
    case 'running':
    case 'deploying':
    case 'building':
    case 'ember':
      return 'ember';
    case 'pending':
    case 'queued':
    case 'awaiting':
    case 'awaiting_approval':
    case 'warn':
    case 'cancelled':
      return 'warn';
    case 'skipped':
      return 'neutral';
    case 'cool':
    case 'info':
      return 'cool';
    default:
      return 'neutral';
  }
}
