import { useTheme } from '../../theme/ThemeProvider';
import { hexA, PLANET_KINDS, type PlanetKind } from '../../theme/tokens';
import { planetKindOf } from './atoms';

interface ConstellationThumbProps {
  /** ordered stage kinds — each becomes a planet dot */
  kinds: string[];
  /** index of the live/running node (gets the ember treatment), if any */
  liveIndex?: number;
  height?: number;
}

/**
 * ConstellationThumb — a mini star-map: planet dots wired by faint dashed
 * trajectories, used on the Pipelines / Templates gallery cards. CSS/SVG only.
 */
export function ConstellationThumb({ kinds, liveIndex, height = 88 }: ConstellationThumbProps) {
  const t = useTheme();
  const VW = 300;
  const VH = 92;
  const list = kinds.length ? kinds.slice(0, 7) : ['custom'];
  const n = list.length;
  const padX = 26;
  const span = n > 1 ? (VW - padX * 2) / (n - 1) : 0;
  // gentle deterministic vertical wave so the chain reads as a constellation
  const points = list.map((k, i) => {
    const x = n > 1 ? padX + i * span : VW / 2;
    const y = VH / 2 + Math.sin(i * 1.1 + 0.6) * (VH * 0.26);
    const kind = planetKindOf(k) as PlanetKind;
    return { x, y, glow: PLANET_KINDS[kind].glow };
  });

  return (
    <svg
      width="100%"
      height={height}
      viewBox={`0 0 ${VW} ${VH}`}
      preserveAspectRatio="xMidYMid meet"
      style={{ display: 'block' }}
      aria-hidden
    >
      {/* dashed trajectories between consecutive planets */}
      {points.slice(1).map((p, i) => {
        const a = points[i];
        const dx = Math.max(24, (p.x - a.x) * 0.5);
        const live = liveIndex === i || liveIndex === i + 1;
        return (
          <path
            key={i}
            d={`M ${a.x} ${a.y} C ${a.x + dx} ${a.y}, ${p.x - dx} ${p.y}, ${p.x} ${p.y}`}
            fill="none"
            stroke={live ? hexA(t.ember, 0.6) : t.lineStrong}
            strokeWidth={live ? 1.6 : 1.2}
            strokeDasharray="2 5"
            strokeLinecap="round"
          />
        );
      })}
      {/* planet dots */}
      {points.map((p, i) => {
        const live = liveIndex === i;
        const c = live ? t.ember : p.glow;
        return (
          <g key={i}>
            <circle cx={p.x} cy={p.y} r={live ? 9 : 7} fill={hexA(c, 0.18)} />
            <circle
              cx={p.x}
              cy={p.y}
              r={live ? 4.5 : 3.6}
              fill={c}
              style={live ? { filter: `drop-shadow(0 0 5px ${t.ember})` } : undefined}
            />
          </g>
        );
      })}
    </svg>
  );
}
