import type { ReactNode, Ref } from 'react';
import Starfield from '../porthole/Starfield';
import AuthStory from './AuthStory';
import CookerMark from '../brand/CookerMark';
import './airlock.css';
import './auth.css';

interface Props {
  title?: ReactNode;
  description?: string;
  titleRef?: Ref<HTMLHeadingElement>;
  children: ReactNode;
  /** Standalone (sign-in) fills the viewport; inside the shell it fills the content area. */
  full?: boolean;
  wide?: boolean;
  seed?: number;
}

/** Full-page authentication uses the accepted Split design; embedded wizards retain their frame. */
export default function Airlock({ title, description, titleRef, children, full = false, wide = false, seed = 3 }: Props) {
  if (full) {
    return (
      <div className="airlock airlock-full airlock-auth">
        <header className="auth-header">
          <a className="auth-wordmark" href="/landing-preview/" aria-label="Cooker overview">
            <CookerMark assemble />
            <span className="auth-wordmark-bracket" aria-hidden="true">[</span> Cooker<span className="auth-wordmark-dot">.</span> <span className="auth-wordmark-bracket" aria-hidden="true">]</span>
          </a>
          <a className="auth-overview" href="/landing-preview/">Back to overview <span aria-hidden="true">↗</span></a>
        </header>
        <main className="auth-layout">
          <section className="auth-entry" aria-label="Your Cooker account">
            <div className="airlock-card">
              <div className="auth-intro">
                <p className="auth-eyebrow">Your Cooker workspace</p>
                {title && <h1 ref={titleRef} tabIndex={titleRef ? -1 : undefined}>{title}</h1>}
                {description && <p className="auth-description">{description}</p>}
              </div>
              {children}
            </div>
          </section>
          <AuthStory />
        </main>
        <footer className="auth-footer">
          <span>Visual by design.</span>
          <span>Your source. Your servers. Your call.</span>
        </footer>
      </div>
    );
  }

  return (
    <div className="airlock">
      <Starfield seed={seed} />
      <div className={wide ? 'airlock-card wide' : 'airlock-card'}>
        <span className="wordmark">cooker</span>
        {title && <h1 ref={titleRef} tabIndex={titleRef ? -1 : undefined}>{title}</h1>}
        {children}
      </div>
    </div>
  );
}
