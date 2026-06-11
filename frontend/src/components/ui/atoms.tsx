import type { CSSProperties, ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA, PLANET_KINDS, type PlanetKind } from '../../theme/tokens';
import type { CookerTheme } from '../../theme/tokens';
import { Icon, type IconName } from './Icon';

export type Tone = 'neutral' | 'accent' | 'good' | 'warn' | 'bad' | 'cool' | 'ember';

// tone → base accent color, resolved against the active (cosmic) theme
export function toneColor(t: CookerTheme, tone: Tone): string {
  const map: Record<Tone, string> = {
    neutral: t.textMute,
    accent: t.violet,
    good: t.good,
    warn: t.warn,
    bad: t.bad,
    cool: t.cool,
    ember: t.ember,
  };
  return map[tone];
}

export function Pill({
  children,
  tone = 'neutral',
  solid = false,
  style,
}: {
  children: ReactNode;
  tone?: Tone;
  /** filled chip (color bg, white text) instead of the tinted-glass default */
  solid?: boolean;
  style?: CSSProperties;
}) {
  const t = useTheme();
  const c = tone === 'neutral' ? t.textMute : toneColor(t, tone);
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        fontFamily: t.mono,
        fontSize: 10,
        letterSpacing: 0.5,
        textTransform: 'uppercase',
        padding: '3px 9px',
        borderRadius: 999,
        background: solid ? c : hexA(c, 0.13),
        color: solid ? '#fff' : c,
        border: `1px solid ${hexA(c, solid ? 0.6 : 0.32)}`,
        whiteSpace: 'nowrap',
        ...style,
      }}
    >
      {children}
    </span>
  );
}

export function StatusDot({
  tone,
  pulse = false,
  size = 8,
}: {
  tone: Tone;
  pulse?: boolean;
  size?: number;
}) {
  const t = useTheme();
  const c = toneColor(t, tone);
  return (
    <span
      style={{
        display: 'inline-block',
        width: size,
        height: size,
        borderRadius: 999,
        background: c,
        boxShadow: pulse ? `0 0 8px ${c}` : 'none',
        animation: pulse ? 'ccPulse 1.6s ease-out infinite' : undefined,
        flexShrink: 0,
      }}
    />
  );
}

export type BtnKind = 'primary' | 'secondary' | 'ghost' | 'ink' | 'danger';

export function Btn({
  children,
  kind = 'ghost',
  icon,
  onClick,
  disabled,
  type = 'button',
  style,
}: {
  children?: ReactNode;
  kind?: BtnKind;
  icon?: IconName;
  onClick?: (e: React.MouseEvent<HTMLButtonElement>) => void;
  disabled?: boolean;
  type?: 'button' | 'submit' | 'reset';
  style?: CSSProperties;
}) {
  const t = useTheme();
  const kinds: Record<BtnKind, CSSProperties> = {
    primary: {
      background: `linear-gradient(135deg, ${t.violet}, ${t.violetGlow})`,
      color: '#fff',
      borderColor: hexA(t.violetGlow, 0.6),
      boxShadow: `0 6px 18px ${hexA(t.violetGlow, 0.38)}`,
    },
    secondary: { background: hexA(t.surfaceSolid, 0.6), color: t.text, borderColor: t.line },
    ghost: { background: 'transparent', color: t.textSoft, borderColor: 'transparent' },
    ink: { background: t.text, color: t.bg, borderColor: t.text },
    danger: { background: hexA(t.bad, 0.13), color: t.bad, borderColor: hexA(t.bad, 0.4) },
  };
  return (
    <button
      type={type}
      className={disabled ? undefined : 'cc-btn'}
      onClick={onClick}
      disabled={disabled}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 7,
        padding: '8px 14px',
        borderRadius: 9,
        fontSize: 12.5,
        fontFamily: t.sans,
        fontWeight: 600,
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.55 : 1,
        border: '1px solid transparent',
        letterSpacing: 0.1,
        transition: 'transform .15s, box-shadow .15s, filter .15s',
        whiteSpace: 'nowrap',
        ...kinds[kind],
        ...style,
      }}
    >
      {icon && <Icon name={icon} size={14} />}
      {children}
    </button>
  );
}

export function KBD({ children }: { children: ReactNode }) {
  const t = useTheme();
  return (
    <kbd
      style={{
        fontFamily: t.mono,
        fontSize: 10.5,
        padding: '2px 6px',
        background: hexA(t.surfaceSolid, 0.6),
        color: t.textSoft,
        borderRadius: 5,
        border: `1px solid ${t.line}`,
        boxShadow: `0 1px 0 ${t.line}`,
        lineHeight: 1,
        display: 'inline-block',
      }}
    >
      {children}
    </kbd>
  );
}

interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  pad?: number;
  /** left accent bar (e.g. stat tiles) */
  accent?: string;
}

export function Card({ children, style, pad = 20, accent, ...rest }: CardProps) {
  const t = useTheme();
  return (
    <div
      {...rest}
      style={{
        background: t.surface,
        backdropFilter: 'blur(10px)',
        WebkitBackdropFilter: 'blur(10px)',
        border: `1px solid ${t.line}`,
        borderLeft: accent ? `3px solid ${accent}` : `1px solid ${t.line}`,
        borderRadius: 14,
        padding: pad,
        boxShadow: `0 10px 30px ${hexA('#000', t.mode === 'dark' ? 0.32 : 0.08)}`,
        ...style,
      }}
    >
      {children}
    </div>
  );
}

export function SectionLabel({ children, style }: { children: ReactNode; style?: CSSProperties }) {
  const t = useTheme();
  return (
    <div
      style={{
        fontFamily: t.mono,
        fontSize: 11,
        letterSpacing: 1.4,
        textTransform: 'uppercase',
        color: t.textMute,
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        ...style,
      }}
    >
      <span>{children}</span>
      <span style={{ flex: 1, height: 1, background: t.line }} />
    </div>
  );
}

export function Field({ label, mono }: { label: string; mono: ReactNode }) {
  const t = useTheme();
  return (
    <div>
      <div
        style={{
          fontFamily: t.mono,
          fontSize: 10,
          color: t.textMute,
          letterSpacing: 1,
          textTransform: 'uppercase',
          marginBottom: 4,
        }}
      >
        {label}
      </div>
      <div
        style={{
          fontFamily: t.mono,
          fontSize: 11.5,
          color: t.text,
          wordBreak: 'break-all',
        }}
      >
        {mono}
      </div>
    </div>
  );
}

export function HealthBar({ value }: { value: number }) {
  const t = useTheme();
  const c = value > 95 ? t.good : value > 80 ? t.warn : t.bad;
  return (
    <div
      style={{
        width: 100,
        height: 5,
        background: t.line,
        borderRadius: 3,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          width: `${Math.max(0, Math.min(100, value))}%`,
          height: '100%',
          background: c,
          borderRadius: 3,
          boxShadow: `0 0 8px ${hexA(c, 0.6)}`,
        }}
      />
    </div>
  );
}

export function Toggle({ on, label, onClick }: { on: boolean; label?: string; onClick?: () => void }) {
  const t = useTheme();
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 8,
        cursor: onClick ? 'pointer' : 'default',
      }}
      onClick={onClick}
    >
      <span
        style={{
          width: 30,
          height: 16,
          borderRadius: 999,
          position: 'relative',
          background: on ? t.good : t.line,
          transition: 'all .2s',
        }}
      >
        <span
          style={{
            position: 'absolute',
            top: 2,
            left: on ? 16 : 2,
            width: 12,
            height: 12,
            borderRadius: 999,
            background: '#fff',
            transition: 'all .2s',
            boxShadow: `0 1px 2px ${hexA('#000', 0.2)}`,
          }}
        />
      </span>
      {label && (
        <span
          style={{
            fontFamily: t.mono,
            fontSize: 10.5,
            color: on ? t.good : t.textMute,
            textTransform: 'uppercase',
            letterSpacing: 0.6,
          }}
        >
          {label}
        </span>
      )}
    </span>
  );
}

// normalize incoming kind strings (e.g. 'approve', 'docker_build') to a PlanetKind
export function planetKindOf(kind?: string): PlanetKind {
  const k = (kind || '').toLowerCase();
  if (k in PLANET_KINDS) return k as PlanetKind;
  if (k.startsWith('approv')) return 'approval';
  if (k.includes('build')) return 'build';
  if (k.includes('test')) return 'test';
  if (k.includes('push') || k.includes('registry')) return 'push';
  if (k.includes('deploy')) return 'deploy';
  if (k.includes('source') || k.includes('clone') || k.includes('checkout')) return 'source';
  return 'custom';
}

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

export function KindBadge({ kind }: { kind: string }) {
  return <PlanetOrb kind={kind} size={26} ring={planetKindOf(kind) === 'approval'} />;
}

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  const t = useTheme();
  const { style, className, ...rest } = props;
  return (
    <input
      {...rest}
      className={className ? `cc-field ${className}` : 'cc-field'}
      style={{
        width: '100%',
        padding: '9px 11px',
        background: hexA(t.surfaceSolid, 0.5),
        color: t.text,
        border: `1px solid ${t.line}`,
        borderRadius: 8,
        fontSize: 13.5,
        fontFamily: t.sans,
        outline: 'none',
        transition: 'border-color .15s, box-shadow .15s',
        ...style,
      }}
    />
  );
}

export function Select(props: React.SelectHTMLAttributes<HTMLSelectElement>) {
  const t = useTheme();
  const { style, className, children, ...rest } = props;
  return (
    <select
      {...rest}
      className={className ? `cc-field ${className}` : 'cc-field'}
      style={{
        width: '100%',
        padding: '9px 11px',
        background: hexA(t.surfaceSolid, 0.5),
        color: t.text,
        border: `1px solid ${t.line}`,
        borderRadius: 8,
        fontSize: 13.5,
        fontFamily: t.sans,
        outline: 'none',
        appearance: 'none',
        transition: 'border-color .15s, box-shadow .15s',
        ...style,
      }}
    >
      {children}
    </select>
  );
}

export function Label({ children }: { children: ReactNode }) {
  const t = useTheme();
  return (
    <label
      style={{
        display: 'block',
        fontFamily: t.mono,
        fontSize: 10.5,
        letterSpacing: 1.2,
        textTransform: 'uppercase',
        color: t.textMute,
        margin: '14px 0 6px',
      }}
    >
      {children}
    </label>
  );
}

export function PageHeader({
  eyebrow,
  title,
  subtitle,
  actions,
}: {
  eyebrow?: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
}) {
  const t = useTheme();
  return (
    <div style={{ display: 'flex', alignItems: 'flex-end', gap: 24, marginBottom: 22 }}>
      <div style={{ minWidth: 0 }}>
        {eyebrow && (
          <div
            style={{
              fontFamily: t.mono,
              fontSize: 10.5,
              letterSpacing: 1.8,
              textTransform: 'uppercase',
              color: t.violet,
              marginBottom: 10,
            }}
          >
            {eyebrow}
          </div>
        )}
        <h1
          style={{
            fontFamily: t.display,
            fontSize: 34,
            lineHeight: 1.05,
            fontWeight: 600,
            color: t.text,
            letterSpacing: -0.8,
            margin: 0,
          }}
        >
          {title}
        </h1>
        {subtitle && (
          <p style={{ fontSize: 14, color: t.textSoft, marginTop: 12, lineHeight: 1.55, maxWidth: 720 }}>
            {subtitle}
          </p>
        )}
      </div>
      <div style={{ flex: 1 }} />
      {actions && <div style={{ display: 'flex', gap: 10 }}>{actions}</div>}
    </div>
  );
}

export function EmptyState({
  title,
  body,
  action,
}: {
  title: ReactNode;
  body?: ReactNode;
  action?: ReactNode;
}) {
  const t = useTheme();
  return (
    <Card style={{ textAlign: 'center', padding: '60px 28px' }}>
      <div
        style={{
          fontFamily: t.serif,
          fontSize: 22,
          fontWeight: 500,
          color: t.text,
          letterSpacing: -0.3,
        }}
      >
        {title}
      </div>
      {body && (
        <div style={{ color: t.textSoft, marginTop: 10, fontSize: 13.5, lineHeight: 1.6 }}>{body}</div>
      )}
      {action && <div style={{ marginTop: 22 }}>{action}</div>}
    </Card>
  );
}

export function statusTone(status?: string): Tone {
  switch (status) {
    case 'success':
    case 'deployed':
    case 'good':
    case 'ok':
      return 'good';
    case 'failed':
    case 'error':
    case 'bad':
      return 'bad';
    case 'running':
    case 'deploying':
    case 'building':
    case 'ember':
      return 'ember';
    case 'pending':
    case 'queued':
    case 'awaiting':
    case 'awaiting_approval':
    case 'warn':
    case 'cancelled':
      return 'warn';
    case 'skipped':
      return 'neutral';
    case 'cool':
    case 'info':
      return 'cool';
    default:
      return 'neutral';
  }
}
