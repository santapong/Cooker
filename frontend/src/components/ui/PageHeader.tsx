import type { ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';

export function PageHeader({
  eyebrow,
  title,
  subtitle,
  actions,
}: {
  eyebrow?: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
}) {
  const t = useTheme();
  return (
    <div style={{ display: 'flex', alignItems: 'flex-end', gap: 24, marginBottom: 22 }}>
      <div style={{ minWidth: 0 }}>
        {eyebrow && (
          <div
            style={{
              fontFamily: t.mono,
              fontSize: 10.5,
              letterSpacing: 1.8,
              textTransform: 'uppercase',
              color: t.violet,
              marginBottom: 10,
            }}
          >
            {eyebrow}
          </div>
        )}
        <h1
          style={{
            fontFamily: t.display,
            fontSize: 34,
            lineHeight: 1.05,
            fontWeight: 600,
            color: t.text,
            letterSpacing: -0.8,
            margin: 0,
          }}
        >
          {title}
        </h1>
        {subtitle && (
          <p style={{ fontSize: 14, color: t.textSoft, marginTop: 12, lineHeight: 1.55, maxWidth: 720 }}>
            {subtitle}
          </p>
        )}
      </div>
      <div style={{ flex: 1 }} />
      {actions && <div style={{ display: 'flex', gap: 10 }}>{actions}</div>}
    </div>
  );
}
