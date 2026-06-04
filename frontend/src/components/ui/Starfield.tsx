import { useMemo } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';

interface StarfieldProps {
  /** deterministic seed so a given surface renders the same field every time */
  seed?: number;
  /** number of stars */
  density?: number;
  /** render the soft nebula bloom behind the stars */
  nebula?: boolean;
  style?: React.CSSProperties;
}

/**
 * Starfield — an absolutely-positioned, pointer-transparent backdrop of CSS
 * stars (+ optional nebula bloom). Twinkle is decorative and gated behind
 * prefers-reduced-motion in index.css via the ccTwinkle keyframe; the visible
 * dots are the base state, so reduced-motion users still see the field.
 */
export function Starfield({ seed = 1, density = 90, nebula = true, style }: StarfieldProps) {
  const t = useTheme();
  const stars = useMemo(() => {
    // tiny LCG so the layout is stable across renders for a given seed
    let s = seed * 9301 + 49297;
    const rnd = () => {
      s = (s * 9301 + 49297) % 233280;
      return s / 233280;
    };
    return Array.from({ length: density }, () => ({
      x: rnd() * 100,
      y: rnd() * 100,
      r: rnd() * 1.4 + 0.3,
      o: rnd() * 0.7 + 0.2,
      d: rnd() * 4 + 2,
      delay: rnd() * 4,
    }));
  }, [seed, density]);

  const dark = t.mode === 'dark';
  return (
    <div style={{ position: 'absolute', inset: 0, overflow: 'hidden', pointerEvents: 'none', ...style }}>
      {nebula && (
        <>
          <div
            style={{
              position: 'absolute',
              width: '60%',
              height: '60%',
              left: '-10%',
              top: '-15%',
              background: `radial-gradient(circle, ${hexA(t.violetGlow, dark ? 0.28 : 0.14)} 0%, transparent 65%)`,
              filter: 'blur(20px)',
            }}
          />
          <div
            style={{
              position: 'absolute',
              width: '55%',
              height: '55%',
              right: '-8%',
              bottom: '-12%',
              background: `radial-gradient(circle, ${hexA(t.cyan, dark ? 0.18 : 0.1)} 0%, transparent 65%)`,
              filter: 'blur(24px)',
            }}
          />
          <div
            style={{
              position: 'absolute',
              width: '45%',
              height: '40%',
              right: '18%',
              top: '-6%',
              background: `radial-gradient(circle, ${hexA(t.ember, dark ? 0.1 : 0.08)} 0%, transparent 60%)`,
              filter: 'blur(26px)',
            }}
          />
        </>
      )}
      {stars.map((st, i) => (
        <span
          key={i}
          style={{
            position: 'absolute',
            left: `${st.x}%`,
            top: `${st.y}%`,
            width: st.r * 2,
            height: st.r * 2,
            borderRadius: 999,
            background: dark ? '#fff' : t.violet,
            opacity: st.o * t.stars,
            boxShadow: dark ? `0 0 ${st.r * 3}px ${hexA('#cdd6ff', 0.8)}` : 'none',
            animation: `ccTwinkle ${st.d}s ease-in-out ${st.delay}s infinite`,
          }}
        />
      ))}
    </div>
  );
}
