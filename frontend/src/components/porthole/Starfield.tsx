import { useMemo } from 'react';
import { makeStars } from './starfield';

interface Props {
  seed?: number;
}

/**
 * Two parallax layers of stars behind a porthole. Drift is a transform on
 * each layer (240 s / 360 s loops); a sparse subset twinkles on a 9 s
 * period. Under reduced motion or Calm mode the field is static — stars
 * remain, motion off (spec §3 substitution table).
 */
export default function Starfield({ seed = 7 }: Props) {
  const layers = useMemo(
    () => [makeStars(seed, 140, 1, 1.6), makeStars(seed + 1, 80, 1.6, 2.4)],
    [seed],
  );
  return (
    <svg
      className="starfield"
      viewBox="0 0 1000 1000"
      preserveAspectRatio="xMidYMid slice"
      aria-hidden="true"
      focusable="false"
    >
      {layers.map((stars, li) => (
        <g key={li} className={`field f${li + 1}`}>
          {stars.map((s, i) => (
            <circle
              key={i}
              className={s.twinkle ? 'dot tw' : 'dot'}
              cx={s.x * 10}
              cy={s.y * 10}
              r={s.size / 2}
              style={{ opacity: s.opacity, animationDelay: s.twinkle ? `${s.delay}s` : undefined }}
            />
          ))}
        </g>
      ))}
    </svg>
  );
}
