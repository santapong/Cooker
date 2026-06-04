import type { CSSProperties } from 'react';
import { hexA, type CookerTheme } from '../../theme/tokens';

// Shared chrome for React Flow "planet"/"service" nodes, so the pipeline and
// compose canvases can't drift apart. Pure functions of the theme — no JSX, so
// this module stays fast-refresh friendly.

/** 9px glowing violet connection port (restyled React Flow <Handle>). */
export function portHandleStyle(t: CookerTheme): CSSProperties {
  return {
    width: 9,
    height: 9,
    background: t.violetGlow,
    border: `1.5px solid ${t.mode === 'dark' ? t.bg : '#fff'}`,
    boxShadow: `0 0 8px ${hexA(t.violetGlow, 0.8)}`,
  };
}

/** Glass node shell: surface + blur + state-driven border/shadow (radius 14). */
export function nodeShellStyle(
  t: CookerTheme,
  { selected = false, live = false }: { selected?: boolean; live?: boolean } = {},
): CSSProperties {
  return {
    background: t.surface,
    backdropFilter: 'blur(10px)',
    WebkitBackdropFilter: 'blur(10px)',
    border: `1px solid ${selected ? t.violet : live ? hexA(t.ember, 0.5) : t.line}`,
    borderRadius: 14,
    color: t.text,
    boxShadow: selected
      ? `0 0 0 3px ${hexA(t.violet, 0.28)}, 0 14px 40px ${hexA('#000', t.mode === 'dark' ? 0.5 : 0.14)}`
      : live
        ? `0 0 26px ${hexA(t.ember, 0.3)}, 0 14px 38px ${hexA('#000', t.mode === 'dark' ? 0.5 : 0.14)}`
        : `0 12px 30px ${hexA('#000', t.mode === 'dark' ? 0.42 : 0.1)}`,
  };
}
