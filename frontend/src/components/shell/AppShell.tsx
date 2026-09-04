import { useEffect, type ReactNode } from 'react';
import { useUIStore } from '../../stores/uiStore';
import InstrumentRail from './InstrumentRail';
import TopStrip from './TopStrip';
import './shell.css';

/**
 * AppShell — the flight deck around every protected route: instrument rail,
 * top strip, content. Publishes Calm mode to CSS via `data-calm` on <html>.
 */
export default function AppShell({ children }: { children: ReactNode }) {
  const calm = useUIStore((s) => s.calmMode);

  useEffect(() => {
    document.documentElement.dataset.calm = calm ? 'true' : 'false';
  }, [calm]);

  return (
    <div className="shell">
      <InstrumentRail />
      <TopStrip />
      <main id="main" className="shell-main">
        {children}
      </main>
    </div>
  );
}
