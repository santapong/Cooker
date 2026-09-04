import type { ReactNode } from 'react';
import Caps from '../ui/Caps';

interface Props {
  label: ReactNode;
  value: ReactNode;
  /** Secondary reading under the value (e.g. "of 12"). */
  sub?: ReactNode;
  tone?: 'default' | 'ok' | 'fail' | 'ember';
}

/** Gauge-style counter for instrument panels (spec §5.C): mono value, small-caps caption. */
export default function Gauge({ label, value, sub, tone = 'default' }: Props) {
  return (
    <div className={`gauge gauge-${tone}`}>
      <Caps>{label}</Caps>
      <span className="gauge-value mono">{value}</span>
      {sub && <span className="gauge-sub mono">{sub}</span>}
    </div>
  );
}
