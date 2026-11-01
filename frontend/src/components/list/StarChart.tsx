import { useEffect, useRef, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react';
import Skeleton from '../ui/Skeleton';
import './starchart.css';

export type ChartStatus = 'idle' | 'ok' | 'fail' | 'running' | 'warn';

export interface ChartRow {
  id: string;
  name: ReactNode;
  sub?: ReactNode;
  /** Mono meta cells, rendered with · separators. */
  meta?: ReactNode[];
  status?: ChartStatus;
  /** Thumbnail slot (mini constellation or a glyph). Omit for text-only lists. */
  thumb?: ReactNode;
  /** Link target. Rows without a target are static. */
  href?: string;
  /** Called on activation (click / Enter) with the row's thumbnail element for the porthole transition. */
  onOpen?: (thumbEl: HTMLElement | null) => void;
  /** Prefetch on hover (e.g. the route chunk). */
  onPrefetch?: () => void;
  trailing?: ReactNode;
}

interface Props {
  title: string;
  count?: number;
  actions?: ReactNode;
  rows: ChartRow[];
  loading?: boolean;
  error?: string | null;
  empty?: { text: string; action?: ReactNode };
  hasThumbs?: boolean;
  children?: ReactNode;
  /** Extra content under the rows (load more, secondary sections). */
  footer?: ReactNode;
}

function Sketch() {
  return (
    <svg className="sketch" viewBox="0 0 220 60" width="220" height="60" aria-hidden="true" focusable="false">
      <path d="M 20 40 Q 60 12 100 24 M 100 24 Q 140 36 180 20 M 100 24 Q 120 50 160 46" />
      {[
        [20, 40],
        [100, 24],
        [180, 20],
        [160, 46],
      ].map(([x, y]) => (
        <circle key={`${x}-${y}`} cx={x} cy={y} r="3" />
      ))}
    </svg>
  );
}

function Row({ row, hasThumbs }: { row: ChartRow; hasThumbs: boolean }) {
  const thumbRef = useRef<HTMLDivElement>(null);
  const isLink = !!row.href;
  const open = (e: MouseEvent | KeyboardEvent) => {
    if (!row.onOpen) return;
    if ('button' in e && (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0)) return; // modified clicks open a new tab
    e.preventDefault();
    row.onOpen(thumbRef.current);
  };
  // Whole row is clickable. The name anchor and any trailing links/buttons
  // handle their own clicks, so the row only reacts to clicks elsewhere.
  const onRowClick = (e: MouseEvent<HTMLDivElement>) => {
    if (!isLink) return;
    const t = e.target as HTMLElement;
    if (t.closest('a, button')) return;
    if (row.onOpen) open(e);
    else if (row.href) window.location.assign(row.href);
  };
  const cls = ['chart-row', isLink ? 'is-link' : '', hasThumbs ? '' : 'no-thumb'].filter(Boolean).join(' ');
  return (
    <div className={cls} onClick={onRowClick} onMouseEnter={row.onPrefetch}>
      {hasThumbs && (
        <div ref={thumbRef} className="chart-thumb">
          {row.thumb}
        </div>
      )}
      <span className={`chart-status st-${row.status ?? 'idle'}`} aria-hidden="true" />
      <div className="chart-main">
        {isLink ? (
          <a className="chart-name" href={row.href} onClick={open} onFocus={row.onPrefetch}>
            {row.name}
          </a>
        ) : (
          <span className="chart-name">{row.name}</span>
        )}
        {row.sub && <span className="chart-sub">{row.sub}</span>}
      </div>
      <div className="chart-meta">
        {row.meta?.map((m, i) => <span key={i}>{m}</span>)}
        {row.trailing}
      </div>
    </div>
  );
}

/** Rows only — for secondary sections under a StarChart. */
export function ChartRows({ rows, hasThumbs = true }: { rows: ChartRow[]; hasThumbs?: boolean }) {
  return (
    <div className="chart-rows">
      {rows.map((row) => (
        <Row key={row.id} row={row} hasThumbs={hasThumbs} />
      ))}
    </div>
  );
}

/**
 * StarChart — the list family from spec §5.A. Hairline rows; a thumbnail
 * (mini constellation) on the left doubles as the shared element that
 * flies into the porthole; status star; display-face name; mono meta.
 */
export default function StarChart({ title, count, actions, rows, loading = false, error, empty, hasThumbs = true, children, footer }: Props) {
  const titleRef = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    titleRef.current?.focus({ preventScroll: true });
  }, []);

  return (
    <section className="chart">
      <header className="chart-head">
        <h1 ref={titleRef} tabIndex={-1}>
          {title}
        </h1>
        {count !== undefined && <span className="chart-count mono">{count}</span>}
        <span className="chart-spacer" />
        {actions && <div className="chart-actions">{actions}</div>}
      </header>
      {children}
      {error && <div className="chart-error" role="alert">{error}</div>}
      {loading && rows.length === 0 ? (
        <div className="chart-rows" role="status" aria-live="polite" aria-label="Loading">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className={hasThumbs ? 'chart-row' : 'chart-row no-thumb'}>
              {hasThumbs && <Skeleton width={72} height={40} radius={4} />}
              <span className="chart-status" aria-hidden="true" />
              <Skeleton width={i % 2 ? 220 : 160} height={14} />
              <Skeleton width={120} height={12} />
            </div>
          ))}
        </div>
      ) : rows.length === 0 && !error ? (
        <div className="chart-empty">
          <div>
            <Sketch />
            <p>{empty?.text ?? 'Nothing here yet.'}</p>
            {empty?.action}
          </div>
        </div>
      ) : (
        <ChartRows rows={rows} hasThumbs={hasThumbs} />
      )}
      {footer}
    </section>
  );
}
