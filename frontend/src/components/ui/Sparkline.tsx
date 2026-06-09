import { useTheme } from '../../theme/ThemeProvider';

/**
 * Sparkline — dependency-free SVG polyline for small trend charts
 * (roadmap M4 analytics). Values <= 0 are treated as gaps (e.g. the
 * -1 "not measurable" sentinel from run durations).
 */
export function sparklinePoints(
  values: number[],
  width: number,
  height: number,
  pad = 2,
): string {
  const usable = values.filter((v) => v > 0);
  if (usable.length === 0 || values.length < 2) return '';
  const min = Math.min(...usable);
  const max = Math.max(...usable);
  const span = max - min || 1;
  const stepX = (width - pad * 2) / (values.length - 1);
  return values
    .map((v, i) => {
      if (v <= 0) return null;
      const x = pad + i * stepX;
      const y = pad + (height - pad * 2) * (1 - (v - min) / span);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .filter((p): p is string => p !== null)
    .join(' ');
}

export function Sparkline({
  values,
  width = 220,
  height = 36,
  tone = 'accent',
}: {
  values: number[];
  width?: number;
  height?: number;
  tone?: 'accent' | 'good' | 'bad';
}) {
  const t = useTheme();
  const color = tone === 'good' ? t.good : tone === 'bad' ? t.bad : t.accent;
  const points = sparklinePoints(values, width, height);
  if (!points) {
    return (
      <span style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
        not enough data
      </span>
    );
  }
  return (
    <svg width={width} height={height} role="img" aria-label="trend">
      <polyline
        points={points}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
