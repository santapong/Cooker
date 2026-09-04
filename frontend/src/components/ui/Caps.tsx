import type { ReactNode } from 'react';

interface Props {
  children: ReactNode;
  className?: string;
  /** Element to render; instrument labels are usually spans or h2s. */
  as?: 'span' | 'div' | 'h2' | 'h3' | 'label' | 'th';
}

/** Small-caps instrument label — `STAGES`, `TELEMETRY`. */
export default function Caps({ children, className, as: Tag = 'span' }: Props) {
  return <Tag className={className ? `caps ${className}` : 'caps'}>{children}</Tag>;
}
