import type { CSSProperties, ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Icon, type IconName } from './Icon';

export type Tone = 'neutral' | 'accent' | 'good' | 'warn' | 'bad' | 'cool' | 'ember';

export function Pill({
  children,
  tone = 'neutral',
  style,
}: {
  children: ReactNode;
  tone?: Tone;
  style?: CSSProperties;
}) {
  const t = useTheme();
  const map: Record<Tone, { bg: string; fg: string; bd: string }> = {
    neutral: { bg: t.surfaceAlt, fg: t.textSoft, bd: t.line },
    accent: { bg: hexA(t.accent, 0.1), fg: t.accent, bd: hexA(t.accent, 0.3) },
    good: { bg: hexA(t.good, 0.12), fg: t.good, bd: hexA(t.good, 0.3) },
    warn: { bg: hexA(t.warn, 0.14), fg: t.warn, bd: hexA(t.warn, 0.3) },
    bad: { bg: hexA(t.bad, 0.12), fg: t.bad, bd: hexA(t.bad, 0.3) },
    cool: { bg: hexA(t.cool, 0.12), fg: t.cool, bd: hexA(t.cool, 0.3) },
    ember: { bg: hexA(t.ember, 0.14), fg: '#7C5511', bd: hexA(t.ember, 0.4) },
  };
  const c = map[tone];
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        fontFamily: t.mono,
        fontSize: 11,
        letterSpacing: 0.4,
        textTransform: 'uppercase',
        padding: '3px 8px',
        borderRadius: 999,
        background: c.bg,
        color: c.fg,
        border: `1px solid ${c.bd}`,
        whiteSpace: 'nowrap',
        ...style,
      }}
    >
      {children}
    </span>
  );
}

export function StatusDot({ tone, pulse = false }: { tone: Tone; pulse?: boolean }) {
  const t = useTheme();
  const palette: Record<Tone, string> = {
    neutral: t.textMute,
    accent: t.accent,
    good: t.good,
    warn: t.warn,
    bad: t.bad,
    cool: t.cool,
    ember: t.ember,
  };
  const c = palette[tone];
  return (
    <span
      style={{
        display: 'inline-block',
        width: 8,
        height: 8,
        borderRadius: 999,
        background: c,
        boxShadow: pulse ? `0 0 0 3px ${hexA(c, 0.25)}` : 'none',
        animation: pulse ? 'cookerPulse 1.6s ease-out infinite' : undefined,
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
      background: t.accent,
      color: '#FFF8EE',
      borderColor: t.accentDeep,
      boxShadow: `inset 0 -1px 0 ${hexA(t.accentDeep, 0.6)}`,
    },
    secondary: { background: t.surface, color: t.text, borderColor: t.line },
    ghost: { background: 'transparent', color: t.textSoft, borderColor: 'transparent' },
    ink: { background: t.text, color: t.bg, borderColor: t.text },
    danger: { background: hexA(t.bad, 0.12), color: t.bad, borderColor: hexA(t.bad, 0.4) },
  };
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 8,
        padding: '8px 14px',
        borderRadius: 8,
        fontSize: 13,
        fontFamily: t.sans,
        fontWeight: 500,
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.55 : 1,
        border: '1px solid transparent',
        letterSpacing: 0.1,
        transition: 'all .15s',
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
        background: t.surface,
        color: t.textSoft,
        borderRadius: 4,
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
}

export function Card({ children, style, pad = 20, ...rest }: CardProps) {
  const t = useTheme();
  return (
    <div
      {...rest}
      style={{
        background: t.surface,
        border: `1px solid ${t.line}`,
        borderRadius: 12,
        padding: pad,
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
        height: 4,
        background: t.line,
        borderRadius: 2,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          width: `${Math.max(0, Math.min(100, value))}%`,
          height: '100%',
          background: c,
          borderRadius: 2,
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
            background: '#FFF8EE',
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

export function KindBadge({ kind }: { kind: string }) {
  const t = useTheme();
  const map: Record<string, { ch: string; c: string }> = {
    source: { ch: 'S', c: t.textSoft },
    build: { ch: 'B', c: t.warn },
    test: { ch: 'T', c: t.cool },
    push: { ch: '↥', c: t.text },
    deploy: { ch: 'D', c: t.accent },
    approval: { ch: '✓', c: t.good },
    approve: { ch: '✓', c: t.good },
    custom: { ch: '*', c: t.textMute },
  };
  const m = map[kind] || map.custom;
  return (
    <span
      style={{
        width: 26,
        height: 26,
        borderRadius: 6,
        display: 'grid',
        placeItems: 'center',
        background: hexA(m.c, 0.1),
        color: m.c,
        border: `1px solid ${hexA(m.c, 0.35)}`,
        fontFamily: t.mono,
        fontWeight: 700,
        fontSize: 12,
      }}
    >
      {m.ch}
    </span>
  );
}

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  const t = useTheme();
  const { style, ...rest } = props;
  return (
    <input
      {...rest}
      style={{
        width: '100%',
        padding: '9px 11px',
        background: t.bg,
        color: t.text,
        border: `1px solid ${t.line}`,
        borderRadius: 7,
        fontSize: 13.5,
        fontFamily: t.sans,
        outline: 'none',
        ...style,
      }}
    />
  );
}

export function Select(props: React.SelectHTMLAttributes<HTMLSelectElement>) {
  const t = useTheme();
  const { style, children, ...rest } = props;
  return (
    <select
      {...rest}
      style={{
        width: '100%',
        padding: '9px 11px',
        background: t.bg,
        color: t.text,
        border: `1px solid ${t.line}`,
        borderRadius: 7,
        fontSize: 13.5,
        fontFamily: t.sans,
        outline: 'none',
        appearance: 'none',
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
              fontSize: 11,
              letterSpacing: 1.6,
              textTransform: 'uppercase',
              color: t.textMute,
              marginBottom: 8,
            }}
          >
            {eyebrow}
          </div>
        )}
        <h1
          style={{
            fontFamily: t.serif,
            fontSize: 36,
            lineHeight: 1.05,
            fontWeight: 500,
            color: t.text,
            letterSpacing: -0.6,
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
    case 'awaiting_approval':
    case 'warn':
    case 'cancelled':
      return 'warn';
    case 'cool':
    case 'info':
      return 'cool';
    default:
      return 'neutral';
  }
}
