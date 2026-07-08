import type { ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';

export function Label({ children }: { children: ReactNode }) {
  const t = useTheme();
  return (
    <label
      style={{
        display: 'block',
        fontFamily: t.mono,
        fontSize: 10.5,
        letterSpacing: 1.2,
        textTransform: 'uppercase',
        color: t.textMute,
        margin: '14px 0 6px',
      }}
    >
      {children}
    </label>
  );
}
