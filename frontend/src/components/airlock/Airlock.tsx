import type { ReactNode } from 'react';
import Starfield from '../porthole/Starfield';
import './airlock.css';

interface Props {
  title?: ReactNode;
  children: ReactNode;
  /** Standalone (sign-in) fills the viewport; inside the shell it fills the content area. */
  full?: boolean;
  wide?: boolean;
  seed?: number;
}

/** The airlock (spec §5.D): a card floating over the drifting starfield. */
export default function Airlock({ title, children, full = false, wide = false, seed = 3 }: Props) {
  return (
    <div className={full ? 'airlock airlock-full' : 'airlock'}>
      <Starfield seed={seed} />
      <div className={wide ? 'airlock-card wide' : 'airlock-card'}>
        <span className="wordmark">cooker</span>
        {title && <h1>{title}</h1>}
        {children}
      </div>
    </div>
  );
}
