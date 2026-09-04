import { useEffect, useRef, useState, type ReactNode, type UIEvent } from 'react';
import Caps from '../ui/Caps';
import type { TelemetryLine } from '../porthole/runState';

interface Props {
  title: string;
  open: boolean;
  onToggle: () => void;
  /** Run-level transitions (when no stage is selected). */
  events?: TelemetryLine[];
  /** Raw log lines (selected stage or runtime). */
  lines?: string[];
  live?: boolean;
  loading?: boolean;
  banner?: string | null;
  modeSwitch?: ReactNode;
  /** Right-aligned header extras — run meta (started, by, app URL). */
  trailing?: ReactNode;
}

/**
 * Collapsible bottom telemetry console — mono, 40% of the porthole when
 * open. Follows the tail until the reader scrolls up.
 */
export default function TelemetryConsole({ title, open, onToggle, events, lines, live = false, loading = false, banner, modeSwitch, trailing }: Props) {
  const bodyRef = useRef<HTMLDivElement>(null);
  const [follow, setFollow] = useState(true);
  const count = lines ? lines.length : (events?.length ?? 0);

  useEffect(() => {
    const el = bodyRef.current;
    if (!el || !follow) return;
    el.scrollTop = el.scrollHeight;
  }, [count, follow, open]);

  const onScroll = (e: UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 12);
  };

  return (
    <section className={open ? 'console is-open' : 'console'} aria-label="Telemetry">
      <div className="console-head">
        <button type="button" className="console-toggle" onClick={onToggle} aria-expanded={open} aria-controls="telemetry-body">
          <span className="console-chevron" aria-hidden="true">
            ⌃
          </span>
          <Caps>Telemetry · {title}</Caps>
        </button>
        <span className={live ? 'console-dot is-live' : 'console-dot'} aria-hidden="true" />
        <span className="console-meta mono">
          {loading ? 'loading…' : `${count} ${count === 1 ? 'line' : 'lines'}`}
        </span>
        <span className="console-spacer" />
        {modeSwitch}
        {trailing}
      </div>
      {open && (
        <div id="telemetry-body" ref={bodyRef} className="console-body" onScroll={onScroll} role="log" aria-live="polite">
          {banner && <div className="console-banner">{banner}</div>}
          {lines
            ? lines.length === 0
              ? !loading && <div className="console-empty">No output.</div>
              : lines.map((l, i) => (
                  <div key={i} className="ln">
                    <span className="m">{l}</span>
                  </div>
                ))
            : (events ?? []).length === 0
              ? <div className="console-empty">Waiting for the first stage to start…</div>
              : (events ?? []).map((ev, i) => (
                  <div key={`${ev.at}-${i}`} className={`ln ln-${ev.tone}`}>
                    <span className="t">{ev.time}</span>
                    <span className="m">{ev.text}</span>
                  </div>
                ))}
        </div>
      )}
    </section>
  );
}
