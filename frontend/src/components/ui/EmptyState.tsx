import type { ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { Card } from './Card';

export function EmptyState({
  title,
  body,
  action,
}: {
  title: ReactNode;
  body?: ReactNode;
  action?: ReactNode;
}) {
  const t = useTheme();
  return (
    <Card style={{ textAlign: 'center', padding: '60px 28px' }}>
      <div
        style={{
          fontFamily: t.serif,
          fontSize: 22,
          fontWeight: 500,
          color: t.text,
          letterSpacing: -0.3,
        }}
      >
        {title}
      </div>
      {body && (
        <div style={{ color: t.textSoft, marginTop: 10, fontSize: 13.5, lineHeight: 1.6 }}>{body}</div>
      )}
      {action && <div style={{ marginTop: 22 }}>{action}</div>}
    </Card>
  );
}
