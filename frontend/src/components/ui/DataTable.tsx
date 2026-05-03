import type { ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';

export interface Column<T> {
  key: string;
  header: ReactNode;
  width?: string;
  align?: 'left' | 'right' | 'center';
  render: (row: T) => ReactNode;
}

interface Props<T> {
  rows: T[];
  columns: Column<T>[];
  rowKey: (row: T) => string;
  empty?: ReactNode;
  onRowClick?: (row: T) => void;
}

export function DataTable<T>({ rows, columns, rowKey, empty, onRowClick }: Props<T>) {
  const t = useTheme();
  if (rows.length === 0) {
    return (
      <div style={{ padding: '32px 18px', color: t.textMute, fontSize: 13, textAlign: 'center' }}>
        {empty ?? 'No rows.'}
      </div>
    );
  }
  const grid = columns.map((c) => c.width ?? 'minmax(120px, 1fr)').join(' ');
  return (
    <div>
      <div style={{ display: 'grid', gridTemplateColumns: grid }}>
        {columns.map((c) => (
          <div
            key={c.key}
            style={{
              padding: '10px 14px',
              fontFamily: t.mono,
              fontSize: 10.5,
              letterSpacing: 1,
              textTransform: 'uppercase',
              color: t.textMute,
              textAlign: c.align ?? 'left',
            }}
          >
            {c.header}
          </div>
        ))}
      </div>
      {rows.map((row, i) => (
        <div
          key={rowKey(row)}
          onClick={onRowClick ? () => onRowClick(row) : undefined}
          style={{
            display: 'grid',
            gridTemplateColumns: grid,
            borderTop: `1px solid ${t.lineSoft}`,
            background: i % 2 === 1 ? hexA(t.line, 0.18) : 'transparent',
            cursor: onRowClick ? 'pointer' : 'default',
          }}
        >
          {columns.map((c) => (
            <div
              key={c.key}
              style={{
                padding: '12px 14px',
                fontSize: 12.5,
                color: t.text,
                textAlign: c.align ?? 'left',
                display: 'flex',
                alignItems: 'center',
                justifyContent:
                  c.align === 'right' ? 'flex-end' : c.align === 'center' ? 'center' : 'flex-start',
              }}
            >
              {c.render(row)}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}
