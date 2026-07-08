import type { CSSProperties } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import AppCard, { type RowApp } from './AppCard';

// Mirrors the private `rowGrid` in AppCard.tsx so the header row lines up
// with each AppCard row — kept in sync manually since it's a 2-branch,
// pure layout helper with no state.
function rowGrid(mode: 'simple' | 'pro', header: boolean): CSSProperties {
  return {
    display: 'grid',
    gridTemplateColumns:
      mode === 'pro'
        ? 'minmax(180px,1.4fr) 200px 200px 80px 130px 90px'
        : 'minmax(180px,1.4fr) 200px 200px 130px 90px',
    alignItems: 'center',
    minHeight: header ? 0 : 50,
  };
}

function colHeads(mode: 'simple' | 'pro'): string[] {
  return mode === 'pro'
    ? ['Service', 'Environment / image', 'Health', 'Runs', 'Last deploy', '']
    : ['Service', 'Environment / image', 'Health', 'Last deploy', ''];
}

interface AppsGridProps {
  rows: RowApp[];
  mode: 'simple' | 'pro';
  onSelect: (appId: string) => void;
}

/** Column-header row plus the list of AppCard rows for the "Your services" table. */
export default function AppsGrid({ rows, mode, onSelect }: AppsGridProps) {
  const t = useTheme();
  return (
    <div>
      <div style={rowGrid(mode, true)}>
        {colHeads(mode).map((h) => (
          <div
            key={h}
            style={{
              fontFamily: t.mono,
              fontSize: 10.5,
              letterSpacing: 1,
              textTransform: 'uppercase',
              color: t.textMute,
              padding: '10px 14px',
            }}
          >
            {h}
          </div>
        ))}
      </div>
      {rows.map((r, i) => (
        <AppCard
          key={r.app.id}
          row={r}
          mode={mode}
          alt={i % 2 === 1}
          onClick={() => onSelect(r.app.id)}
        />
      ))}
    </div>
  );
}
