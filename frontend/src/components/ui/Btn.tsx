import type { CSSProperties, ReactNode } from 'react';
import { useTheme } from '../../theme/ThemeProvider';
import { hexA } from '../../theme/tokens';
import { Icon, type IconName } from './Icon';
import type { BtnKind } from './helpers';

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
