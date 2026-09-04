import type { CSSProperties } from 'react';

interface Props {
  width?: number | string;
  height?: number | string;
  radius?: number;
  className?: string;
  style?: CSSProperties;
}

/**
 * Skeleton block. Motion lives in ui.css (opacity-only breathe; paused
 * under Calm mode). Decorative — the containing region carries `role=status`.
 */
export default function Skeleton({ width = '100%', height = 14, radius, className, style }: Props) {
  return (
    <span
      aria-hidden="true"
      className={className ? `skeleton ${className}` : 'skeleton'}
      style={{ width, height, borderRadius: radius, ...style }}
    />
  );
}
