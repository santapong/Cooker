/**
 * Palette contrast guard — parses tokens.css and checks WCAG ratios so a
 * future palette edit cannot silently drop below AA. Pure arithmetic, no DOM.
 */
import { describe, expect, it } from 'vitest';
import css from './tokens.css?raw';

function token(name: string): string {
  const m = css.match(new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})`));
  if (!m) throw new Error(`token --${name} not found or not a hex colour`);
  return m[1];
}

function luminance(hex: string): number {
  const c = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((v) =>
    v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4,
  );
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
}

function contrast(a: string, b: string): number {
  const [l1, l2] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (l1 + 0.05) / (l2 + 0.05);
}

describe('brand palette contrast', () => {
  const hull0 = token('hull-0');
  const hull1 = token('hull-1');

  it('body ink is AAA on both hull surfaces', () => {
    expect(contrast(token('ink-1'), hull0)).toBeGreaterThanOrEqual(7);
    expect(contrast(token('ink-1'), hull1)).toBeGreaterThanOrEqual(7);
  });

  it('secondary ink is AA for normal text', () => {
    expect(contrast(token('ink-2'), hull0)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(token('ink-2'), hull1)).toBeGreaterThanOrEqual(4.5);
  });

  it('tertiary ink (small-caps labels, 12px) clears the large-text / UI floor', () => {
    expect(contrast(token('ink-3'), hull0)).toBeGreaterThanOrEqual(3);
  });

  it('ember accent is AA as text and as a focus ring', () => {
    expect(contrast(token('ember'), hull0)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(token('ember'), hull1)).toBeGreaterThanOrEqual(3);
  });

  it('semantic status colours are readable on the void', () => {
    const voidHex = token('void');
    expect(contrast(token('star-ok'), voidHex)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(token('star-fail'), voidHex)).toBeGreaterThanOrEqual(4.5);
  });
});
