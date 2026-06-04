import { describe, it, expect } from 'vitest';
import { hexA, cookerTheme, PLANET_KINDS } from './tokens';

describe('hexA', () => {
  it('applies alpha to a #rrggbb hex', () => {
    expect(hexA('#8B6DFF', 0.5)).toBe('rgba(139,109,255,0.5)');
  });

  it('re-tints an existing rgba() string at the new alpha (regression: cosmic line/surface tokens are rgba)', () => {
    expect(hexA('rgba(138, 126, 230, 0.16)', 0.18)).toBe('rgba(138,126,230,0.18)');
  });

  it('re-tints an rgb() string', () => {
    expect(hexA('rgb(20, 21, 52)', 0.72)).toBe('rgba(20,21,52,0.72)');
  });

  it('never produces NaN channels for any theme line/surface token', () => {
    for (const mode of ['dark', 'light'] as const) {
      const t = cookerTheme(mode);
      for (const token of [t.line, t.lineStrong, t.lineSoft, t.surface, t.panelGlass]) {
        expect(hexA(token, 0.3)).not.toContain('NaN');
      }
    }
  });
});

describe('cookerTheme', () => {
  it('returns a complete planet map for both modes', () => {
    for (const mode of ['dark', 'light'] as const) {
      expect(cookerTheme(mode).planets).toBe(PLANET_KINDS);
      expect(cookerTheme(mode).mode).toBe(mode);
    }
  });
});
