import { useMemo, useState } from 'react';
import type { PipelineAnalytics } from '../../types/pipeline';
import { linePath, niceTicks, scaleLinear, tickDuration } from './charts';
import { formatDuration } from '../porthole/runState';
import './charts.css';

type Run = PipelineAnalytics['runs'][number];

interface Props {
  runs: Run[];
  width?: number;
  height?: number;
}

const PAD = { top: 12, right: 12, bottom: 28, left: 52 };

/** Run duration over recent runs — one ember series, hairline axes, crosshair tooltip. */
export default function RunDurationChart({ runs, width = 720, height = 220 }: Props) {
  const data = useMemo(() => [...runs].filter((r) => r.durationMs >= 0).sort((a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt)), [runs]);
  const [hover, setHover] = useState<number | null>(null);
  const plotW = width - PAD.left - PAD.right;
  const plotH = height - PAD.top - PAD.bottom;
  const max = Math.max(0, ...data.map((d) => d.durationMs));
  const ticks = niceTicks(max);
  const yMax = ticks[ticks.length - 1] || 1;
  const sy = scaleLinear(yMax, plotH);
  const step = data.length > 1 ? plotW / (data.length - 1) : 0;
  const points = data.map((d, i) => ({ x: PAD.left + (data.length > 1 ? i * step : plotW / 2), y: PAD.top + plotH - sy(d.durationMs), datum: d }));

  if (data.length === 0) return <p>No completed runs yet.</p>;
  const h = hover !== null ? points[hover] : null;

  return (
    <div className="chart-wrap">
      <svg className="chart-svg" viewBox={`0 0 ${width} ${height}`} role="img" aria-label={`Run duration over the last ${data.length} runs`}>
        {ticks.map((t) => {
          const y = PAD.top + plotH - sy(t);
          return (
            <g key={t}>
              <line className={t === 0 ? 'axis' : 'grid'} x1={PAD.left} x2={PAD.left + plotW} y1={y} y2={y} />
              <text className="tick" x={PAD.left - 8} y={y + 3} textAnchor="end">
                {tickDuration(t)}
              </text>
            </g>
          );
        })}
        <line className="axis" x1={PAD.left} x2={PAD.left} y1={PAD.top} y2={PAD.top + plotH} />
        <text className="tick" x={PAD.left} y={height - 8}>
          {new Date(data[0].createdAt).toLocaleDateString()}
        </text>
        <text className="tick" x={PAD.left + plotW} y={height - 8} textAnchor="end">
          {new Date(data[data.length - 1].createdAt).toLocaleDateString()}
        </text>
        {h && <line className="crosshair" x1={h.x} x2={h.x} y1={PAD.top} y2={PAD.top + plotH} />}
        <path className="series" d={linePath(points)} />
        {points.map((p, i) => (
          <g key={p.datum.runId}>
            <circle className={`mark${p.datum.status === 'failed' ? ' is-fail' : ''}${hover === i ? ' is-hover' : ''}`} cx={p.x} cy={p.y} r={4} />
            <rect className="hit" x={p.x - Math.max(step / 2, 8)} y={PAD.top} width={Math.max(step, 16)} height={plotH} onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)} />
          </g>
        ))}
      </svg>
      {h && (
        <div className="chart-tip" style={{ left: `${(h.x / width) * 100}%`, top: `${(h.y / height) * 100}%` }}>
          {formatDuration(h.datum.durationMs)} <span className="muted">· {h.datum.status} · {new Date(h.datum.createdAt).toLocaleString()}</span>
        </div>
      )}
    </div>
  );
}
