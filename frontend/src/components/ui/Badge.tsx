import type { ReactNode } from 'react';

export type BadgeVariant = 'default' | 'muted' | 'ember' | 'running' | 'ok' | 'fail';

interface Props {
  children: ReactNode;
  variant?: BadgeVariant;
  className?: string;
  title?: string;
}

/** Capsule badge. Status variants map to the star tokens. */
export default function Badge({ children, variant = 'default', className, title }: Props) {
  const cls = ['badge', variant !== 'default' ? `badge-${variant}` : '', className ?? '']
    .filter(Boolean)
    .join(' ');
  return (
    <span className={cls} title={title}>
      {children}
    </span>
  );
}
