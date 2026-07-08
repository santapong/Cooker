import type { ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';

export function KBD({ children }: { children: ReactNode }) {
  const t = useTheme();
  return (
    <kbd
      style={{
        fontFamily: t.mono,
        fontSize: 10.5,
        padding: '2px 6px',
        background: hexA(t.surfaceSolid, 0.6),
        color: t.textSoft,
        borderRadius: 5,
        border: `1px solid ${t.line}`,
        boxShadow: `0 1px 0 ${t.line}`,
        lineHeight: 1,
        display: 'inline-block',
      }}
    >
      {children}
    </kbd>
  );
}
