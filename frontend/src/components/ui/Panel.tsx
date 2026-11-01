import type { ReactNode } from 'react';
import Caps from './Caps';

interface Props {
  title: ReactNode;
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
}

/** Instrument panel — a raised card with a small-caps title (spec §5.C). */
export default function Panel({ title, aside, children, className }: Props) {
  return (
    <section className={className ? `panel ${className}` : 'panel'}>
      <header className="panel-head">
        <Caps as="h2">{title}</Caps>
        {aside && <span className="panel-aside">{aside}</span>}
      </header>
      {children}
    </section>
  );
}
