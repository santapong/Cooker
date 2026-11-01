import { useState } from 'react';
import type { PipelineAnalytics } from '../../types/pipeline';
import { niceTicks, scaleLinear, tickDuration } from './charts';
import { formatDuration } from '../porthole/runState';
import './charts.css';

type StageStat = PipelineAnalytics['stages'][number];

interface Props {
  stages: StageStat[];
  width?: number;
}

const ROW = 26;
const PAD = { top: 8, right: 16, bottom: 24, left: 120 };

/** Per-stage median duration as ember bars with the p95 as a tick mark. One axis, one unit. */
export default function StageDurationChart({ stages, width = 720 }: Props) {
  const [hover, setHover] = useState<number | null>(null);
  const data = [...stages].sort((a, b) => b.p50Ms - a.p50Ms);
  if (data.length === 0) return <p>No stage samples yet.</p>;
  const plotW = width - PAD.left - PAD.right;
  const height = PAD.top + data.length * ROW + PAD.bottom;
  const max = Math.max(...data.map((d) => Math.max(d.p50Ms, d.p95Ms)));
  const ticks = niceTicks(max);
  const xMax = ticks[ticks.length - 1] || 1;
  const sx = scaleLinear(xMax, plotW);

  return (
    <div className="chart-figure">
      <div className="chart-wrap">
        <svg className="chart-svg" viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Median and p95 duration per stage">
          {ticks.map((t) => (
            <g key={t}>
              <line className={t === 0 ? 'axis' : 'grid'} x1={PAD.left + sx(t)} x2={PAD.left + sx(t)} y1={PAD.top} y2={PAD.top + data.length * ROW} />
              <text className="tick" x={PAD.left + sx(t)} y={height - 8} textAnchor="middle">
                {tickDuration(t)}
              </text>
            </g>
          ))}
          {data.map((d, i) => {
            const y = PAD.top + i * ROW;
            const w = Math.max(sx(d.p50Ms), 2);
            return (
              <g key={d.stageId} onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)}>
                <text className="label" x={PAD.left - 10} y={y + ROW / 2 + 4} textAnchor="end">
                  {(d.name ?? d.stageId).slice(0, 16)}
                </text>
                <rect className="bar" x={PAD.left} y={y + 6} width={w} height={ROW - 12} rx={0} style={{ opacity: hover === null || hover === i ? 0.85 : 0.4 }} />
                <line className="bar-p95" x1={PAD.left + sx(d.p95Ms)} x2={PAD.left + sx(d.p95Ms)} y1={y + 4} y2={y + ROW - 4} />
                {hover === i && (
                  <text className="value" x={PAD.left + Math.max(w, sx(d.p95Ms)) + 8} y={y + ROW / 2 + 4}>
                    p50 {formatDuration(d.p50Ms)} · p95 {formatDuration(d.p95Ms)} · {d.samples} runs · {Math.round(d.successRate * 100)}% ok
                  </text>
                )}
                <rect className="hit" x={0} y={y} width={width} height={ROW} />
              </g>
            );
          })}
        </svg>
      </div>
      <div className="chart-legend" aria-hidden="true">
        <span>median</span>
        <span className="p95">p95</span>
      </div>
    </div>
  );
}
