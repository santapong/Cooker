import type { CSSProperties } from 'react';
import type { AppModel } from '../../types/app';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { HealthBar, Pill, StatusDot, type Tone } from '../ui/atoms';
import { Icon } from '../ui/Icon';

export interface RowApp {
  app: AppModel;
  status: Tone;
  env: string;
  envTone: Tone;
  image: string;
  health: number;
  runs: number;
  lastDeploy: string;
  owner: string;
  team: string;
}

/**
 * CSS grid template shared by the AppsGrid header row and every AppCard row.
 * Kept private (duplicated in AppsGrid.tsx) rather than exported, so this
 * file only exports the AppCard component plus the RowApp type — exporting
 * a plain function here too would break the Fast Refresh boundary.
 */
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

interface AppCardProps {
  row: RowApp;
  mode: 'simple' | 'pro';
  /** zebra striping for odd rows — mirrors the previous `i % 2 === 1` rule. */
  alt: boolean;
  onClick: () => void;
}

/** A single app row inside the "Your services" grid (AppsPage → AppsGrid → AppCard). */
export default function AppCard({ row: r, mode, alt, onClick }: AppCardProps) {
  const t = useTheme();
  return (
    <div
      onClick={onClick}
      style={{
        ...rowGrid(mode, false),
        borderTop: `1px solid ${t.lineSoft}`,
        background: alt ? hexA(t.line, 0.18) : 'transparent',
        cursor: 'pointer',
      }}
    >
      <div style={{ padding: '12px 14px', display: 'flex', alignItems: 'center', gap: 10 }}>
        <StatusDot tone={r.status} pulse={r.status === 'ember'} />
        <div>
          <div
            style={{
              fontFamily: t.mono,
              fontSize: 13,
              color: t.text,
              fontWeight: 600,
            }}
          >
            {r.app.name}
          </div>
          <div style={{ fontSize: 11, color: t.textMute, marginTop: 2 }}>
            {r.team}
          </div>
        </div>
      </div>

      <div style={{ padding: '12px 14px', display: 'flex', alignItems: 'center', gap: 8 }}>
        <Pill tone={r.envTone}>{r.env}</Pill>
        <span
          style={{
            fontFamily: t.mono,
            fontSize: 11.5,
            color: t.textSoft,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
          }}
        >
          {r.image}
        </span>
      </div>

      <div style={{ padding: '12px 14px', display: 'flex', alignItems: 'center', gap: 10 }}>
        <HealthBar value={r.health} />
        <span
          style={{
            fontFamily: t.mono,
            fontSize: 11,
            color: t.textSoft,
            minWidth: 40,
          }}
        >
          {r.health}%
        </span>
      </div>

      {mode === 'pro' && (
        <div
          style={{
            padding: '12px 14px',
            fontFamily: t.mono,
            fontSize: 11.5,
            color: t.textSoft,
          }}
        >
          {r.runs}
        </div>
      )}

      <div
        style={{
          padding: '12px 14px',
          fontSize: 12,
          color: r.status === 'ember' ? t.ember : t.textSoft,
          display: 'flex',
          alignItems: 'center',
          gap: 8,
        }}
      >
        {r.lastDeploy}
      </div>

      <div
        style={{
          padding: '12px 14px',
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          justifyContent: 'flex-end',
        }}
      >
        <span
          style={{
            width: 22,
            height: 22,
            borderRadius: 999,
            background: t.accentDeep,
            color: '#fff',
            display: 'grid',
            placeItems: 'center',
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 9.5,
          }}
        >
          {r.owner}
        </span>
        <Icon name="arrow" size={13} style={{ color: t.textMute }} />
      </div>
    </div>
  );
}
