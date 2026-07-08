import type { ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { Pill } from '../../components/ui/atoms';

export function SectionHeader({
  title,
  count,
  action,
}: {
  title: string;
  count: number;
  action?: ReactNode;
}) {
  const t = useTheme();
  return (
    <div
      style={{
        padding: '14px 18px',
        borderBottom: `1px solid ${t.line}`,
        display: 'flex',
        alignItems: 'center',
        gap: 12,
      }}
    >
      <span style={{ fontFamily: t.serif, fontSize: 18, fontWeight: 500, color: t.text }}>
        {title}
      </span>
      <Pill>{count}</Pill>
      <div style={{ flex: 1 }} />
      {action}
    </div>
  );
}
