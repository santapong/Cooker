import type { ReactNode } from 'react';
import Starfield from './Starfield';
import './porthole.css';

interface Props {
  /** Top-left HUD — small-caps title. */
  title?: ReactNode;
  /** Top-right HUD — mono telemetry and/or actions. */
  hudRight?: ReactNode;
  /** Bottom-left HUD — canvas controls (fit, calm). */
  hudBottomLeft?: ReactNode;
  children: ReactNode;
  className?: string;
  starfieldSeed?: number;
}

/**
 * The porthole: a window into space cut into the hull. 24px rounded frame,
 * inner rim light, HUD corner brackets, drifting starfield, and a scene
 * layer for whatever constellation the page draws inside.
 */
export default function Porthole({ title, hudRight, hudBottomLeft, children, className, starfieldSeed }: Props) {
  return (
    <section className={className ? `porthole porthole-enter ${className}` : 'porthole porthole-enter'}>
      <Starfield seed={starfieldSeed} />
      <span className="bracket tl" aria-hidden="true" />
      <span className="bracket tr" aria-hidden="true" />
      <span className="bracket bl" aria-hidden="true" />
      <span className="bracket br" aria-hidden="true" />
      <div className="porthole-scene">{children}</div>
      {title && <div className="hud hud-tl">{title}</div>}
      {hudRight && <div className="hud hud-tr">{hudRight}</div>}
      {hudBottomLeft && <div className="hud hud-bl">{hudBottomLeft}</div>}
    </section>
  );
}
