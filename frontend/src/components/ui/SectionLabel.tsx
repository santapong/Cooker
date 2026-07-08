import type { CSSProperties, ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';

export function SectionLabel({ children, style }: { children: ReactNode; style?: CSSProperties }) {
  const t = useTheme();
  return (
    <div
      style={{
        fontFamily: t.mono,
        fontSize: 11,
        letterSpacing: 1.4,
        textTransform: 'uppercase',
        color: t.textMute,
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        ...style,
      }}
    >
      <span>{children}</span>
      <span style={{ flex: 1, height: 1, background: t.line }} />
    </div>
  );
}
