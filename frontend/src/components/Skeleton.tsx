import { type CSSProperties } from 'react'

interface SkeletonProps {
  /** width  height accept any CSS length: 100, '50%', '12rem', etc. Numbers are pixel-equivalents. */
  width?: number | string
  height?: number | string
  /** Apply a circular mask (avatars, status dots). */
  circle?: boolean
  /** className for layout overrides; the shimmer animation comes from the base style. */
  className?: string
  style?: CSSProperties
}

/**
 * Skeleton renders a content placeholder with a subtle shimmer
 * animation. Use it instead of "Loading..." text while data is in
 * flight or auth is restoring. Closes the loading-skeletons portion
 * of backlog item P5.
 *
 * The shimmer animation is defined inline so this component has no
 * external CSS dependency; pages that already have a CSS module can
 * pass a className to override layout.
 */
export default function Skeleton({
  width = '100%',
  height = 16,
  circle = false,
  className,
  style,
}: SkeletonProps) {
  const merged: CSSProperties = {
    display: 'inline-block',
    width: typeof width === 'number' ? `${width}px` : width,
    height: typeof height === 'number' ? `${height}px` : height,
    background:
      'linear-gradient(90deg, rgba(148,163,184,0.10) 0%, rgba(148,163,184,0.25) 50%, rgba(148,163,184,0.10) 100%)',
    backgroundSize: '200% 100%',
    borderRadius: circle ? '50%' : 6,
    animation: 'cooker-skeleton 1.4s ease-in-out infinite',
    ...style,
  }
  return (
    <>
      <span className={className} style={merged} aria-hidden="true" />
      <style>{`@keyframes cooker-skeleton { 0% { background-position: 100% 0 } 100% { background-position: -100% 0 } }`}</style>
    </>
  )
}

/**
 * SkeletonStack renders n stacked Skeleton lines. Useful for list /
 * table placeholders where the row shape is repeating.
 */
export function SkeletonStack({
  rows = 3,
  height = 16,
  gap = 12,
  className,
}: {
  rows?: number
  height?: number | string
  gap?: number
  className?: string
}) {
  return (
    <div className={className} style={{ display: 'flex', flexDirection: 'column', gap }}>
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} height={height} />
      ))}
    </div>
  )
}
