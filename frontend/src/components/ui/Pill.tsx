import type { CSSProperties, ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { toneColor, type Tone } from './helpers';

export function Pill({
  children,
  tone = 'neutral',
  solid = false,
  style,
}: {
  children: ReactNode;
  tone?: Tone;
  /** filled chip (color bg, white text) instead of the tinted-glass default */
  solid?: boolean;
  style?: CSSProperties;
}) {
  const t = useTheme();
  const c = tone === 'neutral' ? t.textMute : toneColor(t, tone);
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        fontFamily: t.mono,
        fontSize: 10,
        letterSpacing: 0.5,
        textTransform: 'uppercase',
        padding: '3px 9px',
        borderRadius: 999,
        background: solid ? c : hexA(c, 0.13),
        color: solid ? '#fff' : c,
        border: `1px solid ${hexA(c, solid ? 0.6 : 0.32)}`,
        whiteSpace: 'nowrap',
        ...style,
      }}
    >
      {children}
    </span>
  );
}
