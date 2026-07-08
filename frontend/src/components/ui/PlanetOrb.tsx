import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { planetKindOf } from './helpers';

/**
 * PlanetOrb — a stage `kind` rendered as a planet: radial-gradient body, halo,
 * unicode glyph, optional Saturn ring; live status adds an ember halo + orbit
 * ring (gated behind reduced-motion via the animations being decorative).
 */
export function PlanetOrb({
  kind,
  size = 36,
  status,
  ring = false,
}: {
  kind: string;
  size?: number;
  status?: string;
  ring?: boolean;
}) {
  const t = useTheme();
  const k = t.planets[planetKindOf(kind)];
  const live = status === 'running' || status === 'building' || status === 'deploying';
  const haloColor = live ? t.ember : k.glow;
  return (
    <div style={{ position: 'relative', width: size, height: size, flexShrink: 0 }}>
      {/* halo */}
      <div
        style={{
          position: 'absolute',
          inset: -size * 0.34,
          borderRadius: 999,
          background: `radial-gradient(circle, ${hexA(haloColor, live ? 0.55 : 0.34)} 0%, transparent 70%)`,
          animation: live ? 'ccHalo 1.8s ease-in-out infinite' : 'none',
          pointerEvents: 'none',
        }}
      />
      {/* orbit ring for live / explicit ring */}
      {(live || ring) && (
        <div
          style={{
            position: 'absolute',
            inset: -size * 0.22,
            borderRadius: 999,
            border: `1px solid ${hexA(live ? t.ember : k.glow, 0.5)}`,
            transform: ring && !live ? 'rotateX(72deg)' : 'none',
            animation: live ? 'ccSpin 6s linear infinite' : 'none',
            pointerEvents: 'none',
          }}
        >
          {live && (
            <span
              style={{
                position: 'absolute',
                top: -2,
                left: '50%',
                width: 4,
                height: 4,
                borderRadius: 999,
                background: t.ember,
                boxShadow: `0 0 6px ${t.ember}`,
              }}
            />
          )}
        </div>
      )}
      {/* planet body */}
      <div
        style={{
          position: 'absolute',
          inset: 0,
          borderRadius: 999,
          background: `radial-gradient(circle at 33% 28%, ${k.from} 0%, ${k.to} 72%, ${hexA(k.to, 0.7)} 100%)`,
          boxShadow: `inset -3px -4px 8px ${hexA('#000', 0.35)}, inset 2px 2px 5px ${hexA('#fff', 0.4)}, 0 0 ${size * 0.4}px ${hexA(haloColor, 0.5)}`,
          display: 'grid',
          placeItems: 'center',
          color: hexA('#1a1030', 0.7),
          fontSize: size * 0.42,
          fontWeight: 700,
        }}
      >
        <span style={{ opacity: 0.55, textShadow: `0 1px 1px ${hexA('#fff', 0.3)}` }}>{k.ch}</span>
      </div>
      {/* saturn-style ring overlay */}
      {ring && (
        <div
          style={{
            position: 'absolute',
            left: -size * 0.34,
            top: '50%',
            width: size * 1.68,
            height: size * 0.5,
            marginTop: -size * 0.25,
            borderRadius: '50%',
            border: `${Math.max(2, size * 0.07)}px solid ${hexA(k.glow, 0.7)}`,
            borderTopColor: hexA(k.glow, 0.25),
            borderBottomColor: hexA(k.glow, 0.9),
            transform: 'rotate(-18deg)',
            pointerEvents: 'none',
          }}
        />
      )}
    </div>
  );
}
