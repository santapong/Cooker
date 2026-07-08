import type { ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';

export function Field({ label, mono }: { label: string; mono: ReactNode }) {
  const t = useTheme();
  return (
    <div>
      <div
        style={{
          fontFamily: t.mono,
          fontSize: 10,
          color: t.textMute,
          letterSpacing: 1,
          textTransform: 'uppercase',
          marginBottom: 4,
        }}
      >
        {label}
      </div>
      <div
        style={{
          fontFamily: t.mono,
          fontSize: 11.5,
          color: t.text,
          wordBreak: 'break-all',
        }}
      >
        {mono}
      </div>
    </div>
  );
}
