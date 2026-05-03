import type { CSSProperties } from 'react';

export type IconName =
  | 'apps'
  | 'pipe'
  | 'ship'
  | 'layers'
  | 'servers'
  | 'flask'
  | 'play'
  | 'pause'
  | 'plus'
  | 'search'
  | 'bell'
  | 'arrow'
  | 'check'
  | 'dot'
  | 'close'
  | 'cog'
  | 'spark'
  | 'wand'
  | 'logout'
  | 'sun'
  | 'moon'
  | 'box'
  | 'docker'
  | 'compose';

const PATHS: Record<IconName, string> = {
  apps: 'M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z',
  pipe: 'M4 6h7M13 6h7M4 12h16M4 18h7M13 18h7',
  ship: 'M3 13l9-7 9 7M5 13v7h14v-7M9 20v-4h6v4',
  layers: 'M12 3l9 5-9 5-9-5 9-5zM3 13l9 5 9-5M3 18l9 5 9-5',
  servers: 'M3 5h18v6H3zM3 13h18v6H3zM6 8h.01M6 16h.01',
  flask: 'M9 3v6L4 19a2 2 0 002 3h12a2 2 0 002-3l-5-10V3M8 3h8',
  play: 'M6 4l14 8-14 8z',
  pause: 'M6 4h4v16H6zM14 4h4v16h-4z',
  plus: 'M12 5v14M5 12h14',
  search: 'M11 11a6 6 0 1 0 0-12 6 6 0 0 0 0 12zM21 21l-5-5',
  bell: 'M6 8a6 6 0 1 1 12 0c0 7 3 9 3 9H3s3-2 3-9zM10 21h4',
  arrow: 'M5 12h14M13 6l6 6-6 6',
  check: 'M5 12l5 5L20 7',
  dot: 'M12 12m-3 0a3 3 0 1 0 6 0a3 3 0 1 0 -6 0',
  close: 'M6 6l12 12M6 18L18 6',
  cog: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8zM19 12l2-1-2-3-2 1-2-1V6h-4v2l-2 1-2-1-2 3 2 1v2l-2 1 2 3 2-1 2 1v2h4v-2l2-1 2 1 2-3-2-1z',
  spark: 'M12 3l2 6 6 2-6 2-2 6-2-6-6-2 6-2z',
  wand: 'M15 4l5 5L9 20l-5 1 1-5L15 4z',
  logout: 'M15 17l5-5-5-5M9 12h11M9 4H5a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4',
  sun: 'M12 4v2M12 18v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z',
  moon: 'M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z',
  box: 'M3 7l9-4 9 4-9 4-9-4zM3 7v10l9 4 9-4V7M12 11v10',
  docker: 'M3 12h18M5 8h4M11 8h4M5 12h4M11 12h4M5 16h14',
  compose: 'M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z',
};

interface Props {
  name: IconName;
  size?: number;
  stroke?: number;
  style?: CSSProperties;
}

export function Icon({ name, size = 16, stroke = 1.6, style }: Props) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={stroke}
      strokeLinecap="round"
      strokeLinejoin="round"
      style={style}
      aria-hidden
    >
      <path d={PATHS[name]} />
    </svg>
  );
}

export function CookerMark({ color, size = 28 }: { color: string; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden>
      <circle cx="16" cy="18" r="11" fill="none" stroke={color} strokeWidth="2.2" />
      <path d="M5 11h22" stroke={color} strokeWidth="2.2" strokeLinecap="round" />
      <circle cx="11" cy="11" r="1.6" fill={color} />
      <circle cx="16" cy="11" r="1.6" fill={color} />
      <circle cx="21" cy="11" r="1.6" fill={color} />
    </svg>
  );
}
